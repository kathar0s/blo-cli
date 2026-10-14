// Package billing is the high-level domain layer over the raw AJAX client.
// It resolves the customer/group context and exposes typed cost queries that
// map to what the CLI commands need (summary, monthly trend, per-service and
// per-account breakdowns). Endpoint and field names are documented in ANALYSIS.md.
package billing

import (
	"bytes"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kathar0s/blo-cli/internal/client"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"
)

// Endpoint paths.
const (
	epListCustomer = "/billingon/common/listCustomerAjax.do"
	epListGroup    = "/billingon/common/listGroupIdForCustomerAjax.do"
	epUsageDates   = "/billingon/common/listGroupUsageDateAjax.do"
	epCostSummary  = "/billingon/dashboard/customerCostSummaryAjax.do"
	epMonthlyTrend = "/billingon/dashboard/monthlyTrendAjax.do"
	epDailyTrend   = "/billingon/dashboard/dailyTrendAjax.do"
	epProductSum   = "/billingon/dashboard/customerProductSumAjax.do"
	epAccountSum   = "/billingon/dashboard/customerAccountSumAjax.do"
	epCostExplorer = "/billingon/invoice/listCostExplorerProductAjax.do"
	epInvoice      = "/billingon/invoice/listInvoiceAjax.do"
	epInvoiceExcel = "/billingon/invoice/invoiceDetailMakeExcelAjax.do"
)

// Service provides typed cost queries.
type Service struct {
	c             *client.Client
	customerIdx   string
	groupID       string
	groupExplicit bool // true when the caller pinned a specific group via New
	partnerIdx    string
	companyName   string
}

// New builds a Service. customerIdx/groupID may be empty to auto-resolve.
// A non-empty groupID is treated as an explicit pin (used by export to decide
// whether to iterate all groups or just the one).
func New(c *client.Client, customerIdx, groupID string) *Service {
	return &Service{c: c, customerIdx: customerIdx, groupID: groupID, groupExplicit: groupID != ""}
}

// Context exposes the resolved identifiers.
func (s *Service) Context() (customerIdx, groupID string) { return s.customerIdx, s.groupID }

// --- resolver ---

type Customer struct {
	CustomerIdx string `json:"customerIdx"`
	CompanyName string `json:"companyName"`
	PartnerName string `json:"partnerName"`
	PartnerIdx  string `json:"partnerIdx"`
}

type Group struct {
	GroupID   string `json:"groupIdForCustomer"`
	GroupName string `json:"groupNmForCustomer"`
}

type customerListResp struct {
	CustomerList []Customer `json:"customerList"`
}
type groupListResp struct {
	GroupList []Group `json:"groupIdList"`
}

// Customers lists the customers visible to the account.
func (s *Service) Customers() ([]Customer, error) {
	var r customerListResp
	err := s.c.Call(epListCustomer, "GET", map[string]string{"partnerIdx": "", "approveFlag": "Y"}, &r)
	return validCustomers(r.CustomerList), err
}

// Groups lists the groups under the given customer.
func (s *Service) Groups(customerIdx string) ([]Group, error) {
	var r groupListResp
	err := s.c.Call(epListGroup, "GET", map[string]string{
		"partnerIdx": "", "customerIdx": customerIdx, "approveFlag": "Y",
	}, &r)
	return validGroups(r.GroupList), err
}

// Resolve fills in customerIdx/groupID by querying the portal when they are not
// already set. The default group is the one whose name matches the company
// (the consolidated "all accounts" group), falling back to the first.
func (s *Service) Resolve() error {
	if s.customerIdx == "" {
		cs, err := s.Customers()
		if err != nil {
			return err
		}
		if len(cs) == 0 {
			return errNoCustomer
		}
		s.customerIdx = cs[0].CustomerIdx
		s.partnerIdx = cs[0].PartnerIdx
		s.companyName = cs[0].CompanyName
	}
	if s.groupID == "" {
		gs, err := s.Groups(s.customerIdx)
		if err != nil {
			return err
		}
		if len(gs) == 0 {
			return errNoGroup
		}
		s.groupID = pickDefaultGroup(gs).GroupID
	}
	return nil
}

// Month-format convention: the CLI surface always uses YYYY-MM (hyphenated).
// The portal is inconsistent — most endpoints accept YYYY-MM, but a few
// (e.g. the Cost Explorer endpoint) want YYYYMM. We absorb that difference
// here, in the billing layer, so commands never deal with the bare format.
// Use monthHyphen / monthCompact rather than formatting inline.

// monthHyphen normalizes any accepted month input to YYYY-MM.
func monthHyphen(month string) string {
	if len(month) == 6 && !strings.Contains(month, "-") { // YYYYMM -> YYYY-MM
		return month[:4] + "-" + month[4:]
	}
	return month
}

// monthCompact normalizes to YYYYMM (no hyphen), for endpoints that require it.
func monthCompact(month string) string {
	return strings.ReplaceAll(month, "-", "")
}

func (s *Service) baseParams(month string) map[string]string {
	return map[string]string{
		"searchDate":         monthHyphen(month),
		"customerIdx":        s.customerIdx,
		"groupIdForCustomer": s.groupID,
	}
}

// cacheTTL decides how long a query for month (YYYY-MM) may be served from the
// on-disk cache. The current (or any future) month changes continuously, so it
// is never cached. The immediately previous month may still settle while the
// invoice finalizes, so it is cached briefly. Older months are immutable and
// cached permanently. The return value feeds client.CallCached/DownloadFormCached
// (0 = no cache, >0 = TTL, <0 = permanent).
func cacheTTL(month string) time.Duration {
	now := time.Now()
	y, m, _ := now.Date()
	firstOfThis := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
	if month == "" || month >= firstOfThis.Format("2006-01") {
		return 0
	}
	if month == firstOfThis.AddDate(0, -1, 0).Format("2006-01") {
		return 24 * time.Hour
	}
	return -1
}

// --- cost summary ---

type CostSummary struct {
	Month         string  `json:"month"`
	CurrentCost   float64 `json:"currentCost"`
	CurrentFrom   string  `json:"currentFrom"`
	CurrentTo     string  `json:"currentTo"`
	PreviousCost  float64 `json:"previousCost"`
	PreviousFrom  string  `json:"previousFrom"`
	PreviousTo    string  `json:"previousTo"`
	EstimatedCost float64 `json:"estimatedCost"`
}

// DeltaPct returns the month-over-month change percentage (vs previous).
func (c CostSummary) DeltaPct() float64 {
	if c.PreviousCost == 0 {
		return 0
	}
	return (c.CurrentCost - c.PreviousCost) / c.PreviousCost * 100
}

type costSummaryResp struct {
	Data struct {
		BeforeData struct {
			BeforeCostsum float64 `json:"before_costsum"`
			BeforeFrom    string  `json:"before_from"`
			BeforeTo      string  `json:"before_to"`
		} `json:"beforeData"`
		EstimatedCost float64 `json:"estimatedCost"`
		SearchData    struct {
			SearchFrom    string  `json:"search_from"`
			SearchCostsum float64 `json:"search_costsum"`
			SearchTo      string  `json:"search_to"`
		} `json:"searchData"`
	} `json:"data"`
}

func (s *Service) CostSummary(month string) (*CostSummary, error) {
	var r costSummaryResp
	if err := s.c.CallCached(epCostSummary, "POST", s.baseParams(month), &r, cacheTTL(month)); err != nil {
		return nil, err
	}
	d := r.Data
	return &CostSummary{
		Month:         month,
		CurrentCost:   d.SearchData.SearchCostsum,
		CurrentFrom:   d.SearchData.SearchFrom,
		CurrentTo:     d.SearchData.SearchTo,
		PreviousCost:  d.BeforeData.BeforeCostsum,
		PreviousFrom:  d.BeforeData.BeforeFrom,
		PreviousTo:    d.BeforeData.BeforeTo,
		EstimatedCost: d.EstimatedCost,
	}, nil
}

// --- breakdowns ---

// LineItem is a generic (name, cost) row used for service/account breakdowns.
type LineItem struct {
	Name string  `json:"name"`
	Cost float64 `json:"cost"`
}

type productSumResp struct {
	ProductSum struct {
		TotCostList []struct {
			ProductName string  `json:"productName"`
			TotCost     float64 `json:"totCost"`
		} `json:"totCostList"`
	} `json:"productSum"`
}

func (s *Service) ServiceBreakdown(month string) ([]LineItem, error) {
	var r productSumResp
	if err := s.c.CallCached(epProductSum, "POST", s.baseParams(month), &r, cacheTTL(month)); err != nil {
		return nil, err
	}
	out := make([]LineItem, 0, len(r.ProductSum.TotCostList))
	for _, it := range r.ProductSum.TotCostList {
		out = append(out, LineItem{Name: it.ProductName, Cost: it.TotCost})
	}
	sortByCostDesc(out)
	return out, nil
}

type costExplorerResp struct {
	ListCostExplorerItem []struct {
		ProductName string  `json:"productName"`
		CostUsd     float64 `json:"costUsd"`
	} `json:"listCostExplorerItem"`
}

// ServiceBreakdownByAccount returns the per-service cost for a specific set of
// AWS account numbers, via the Cost Explorer-style endpoint. When accountNos is
// empty, all accounts under the resolved group are used (equivalent to the
// plain ServiceBreakdown, but routed through this endpoint).
//
// Note: this endpoint expects searchDate as YYYYMM (no hyphen), unlike the
// dashboard endpoints which use YYYY-MM.
func (s *Service) ServiceBreakdownByAccount(month string, accountNos []string) ([]LineItem, error) {
	if len(accountNos) == 0 {
		accts, err := s.AccountBreakdown(month)
		if err != nil {
			return nil, err
		}
		for _, a := range accts {
			accountNos = append(accountNos, a.AccountNo)
		}
	}
	params := map[string]string{
		"searchDate":         monthCompact(month),
		"customerIdx":        s.customerIdx,
		"groupIdForCustomer": s.groupID,
		"partnerIdx":         "",
		"searchValue":        "",
		"arrAccountIdStr":    strings.Join(accountNos, ","),
	}
	var r costExplorerResp
	if err := s.c.CallCached(epCostExplorer, "POST", params, &r, cacheTTL(month)); err != nil {
		return nil, err
	}
	out := make([]LineItem, 0, len(r.ListCostExplorerItem))
	for _, it := range r.ListCostExplorerItem {
		if it.CostUsd == 0 {
			continue
		}
		out = append(out, LineItem{Name: it.ProductName, Cost: it.CostUsd})
	}
	sortByCostDesc(out)
	return out, nil
}

// --- invoice (actual billed amount, per account × service) ---

// InvoiceItem is one account × service billed line from the invoice.
// This reflects the actual invoiced amount (not the usage-based figure that
// cost account / cost summary report). Amounts arrive as comma-formatted
// strings from the server and are parsed to floats here.
type InvoiceItem struct {
	AccountNo string  `json:"accountNo"`
	Alias     string  `json:"alias"`
	Service   string  `json:"service"`
	CostUSD   float64 `json:"costUsd"`
	Credits   float64 `json:"credits"`
}

type invoiceResp struct {
	FinalInvoiceList []struct {
		AccountID   string `json:"accountId"`
		Mail        string `json:"mail"`
		ProductName string `json:"productName"`
		CostUSD     string `json:"costUSD"`
		Credits     string `json:"credits"`
	} `json:"finalInvoiceList"`
}

// Invoice returns the actual invoiced amounts for the month, one row per
// account × service. When accountNos is empty, all accounts in the group are
// requested. The invoice endpoint returns the full list (not paginated here).
func (s *Service) Invoice(month string, accountNos []string) ([]InvoiceItem, error) {
	if len(accountNos) == 0 {
		accts, err := s.AccountBreakdown(month)
		if err != nil {
			return nil, err
		}
		for _, a := range accts {
			accountNos = append(accountNos, a.AccountNo)
		}
	}
	params := map[string]string{
		"providerIdx":        "1",
		"provider":           "aws",
		"page":               "1",
		"sort":               "costUsd",
		"sortBy":             "desc",
		"visibleList":        "100000", // request all rows; server returns full list
		"searchDate":         monthHyphen(month),
		"customerIdx":        s.customerIdx,
		"groupIdForCustomer": s.groupID,
		"partnerIdx":         "",
		"rdoVersion":         "v3",
		"arrAccountIdStr":    strings.Join(accountNos, ","),
	}
	var r invoiceResp
	if err := s.c.CallCached(epInvoice, "POST", params, &r, cacheTTL(month)); err != nil {
		return nil, err
	}
	out := make([]InvoiceItem, 0, len(r.FinalInvoiceList))
	for _, it := range r.FinalInvoiceList {
		no, alias := it.AccountID, it.Mail
		out = append(out, InvoiceItem{
			AccountNo: no,
			Alias:     alias,
			Service:   it.ProductName,
			CostUSD:   parseMoney(it.CostUSD),
			Credits:   parseMoney(it.Credits),
		})
	}
	return out, nil
}

// Account is one AWS account's cost. The portal encodes accountId as
// "<accountNo>|<email>"; we split it for readability.
type Account struct {
	AccountNo string  `json:"accountNo"`
	Alias     string  `json:"alias"`
	Cost      float64 `json:"cost"`
}

type accountSumResp struct {
	AccountSum struct {
		TotCostList []struct {
			AccountID string  `json:"accountId"`
			TotCost   float64 `json:"totCost"`
		} `json:"totCostList"`
	} `json:"accountSum"`
}

func (s *Service) AccountBreakdown(month string) ([]Account, error) {
	var r accountSumResp
	if err := s.c.CallCached(epAccountSum, "POST", s.baseParams(month), &r, cacheTTL(month)); err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(r.AccountSum.TotCostList))
	for _, it := range r.AccountSum.TotCostList {
		no, alias := splitAccountID(it.AccountID)
		out = append(out, Account{AccountNo: no, Alias: alias, Cost: it.TotCost})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return out, nil
}

// --- monthly trend ---

type MonthPoint struct {
	Month     string  `json:"month"`
	Cost      float64 `json:"cost"`
	Estimated float64 `json:"estimated,omitempty"`
}

type monthlyTrendResp struct {
	TrendData []struct {
		UseDt     string  `json:"useDt"`
		Amount    float64 `json:"amount"`
		AmountUsd float64 `json:"amountUsd"`
		EAmount   float64 `json:"eAmount"`
	} `json:"trendData"`
}

// MonthlyTrend returns the month-by-month cost trend, consolidated across every
// account. Like the billing-detail CSV, the trend data lives per group and which
// group holds a given month's data shifts over time: a unified "all accounts"
// group in recent months, but historically one group per account. The groups are
// mutually exclusive per month (verified — no month reports nonzero cost in both
// the parent group and a child), so querying only the resolved group silently
// drops the accounts whose data sits in sibling groups for older months. We fetch
// every group and sum by month; with an explicit group (-g) we honor that single
// group instead.
func (s *Service) MonthlyTrend(month string) ([]MonthPoint, error) {
	if s.groupExplicit {
		return s.monthlyTrendForGroup(month, s.groupID)
	}
	groups, err := s.Groups(s.customerIdx)
	if err != nil {
		return nil, err
	}

	// Fetch each group's trend concurrently (bounded). Trend is uncached by
	// design, so this is N live calls per invocation.
	type result struct {
		pts []MonthPoint
		err error
	}
	results := make([]result, len(groups))
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, g := range groups {
		wg.Add(1)
		go func(i int, groupID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pts, err := s.monthlyTrendForGroup(month, groupID)
			results[i] = result{pts: pts, err: err}
		}(i, g.GroupID)
	}
	wg.Wait()

	// Sum by month, preserving first-seen month order across all groups.
	order := make([]string, 0)
	sum := make(map[string]*MonthPoint)
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		for _, p := range r.pts {
			agg, ok := sum[p.Month]
			if !ok {
				agg = &MonthPoint{Month: p.Month}
				sum[p.Month] = agg
				order = append(order, p.Month)
			}
			agg.Cost += p.Cost
			agg.Estimated += p.Estimated
		}
	}
	out := make([]MonthPoint, 0, len(order))
	for _, m := range order {
		out = append(out, *sum[m])
	}
	return out, nil
}

// MonthlyTrendForYear returns the Jan–Dec trend for a calendar year, consolidated
// across all accounts. The endpoint pins the result to the searchDate's calendar
// year but truncates the series at the searchDate month, so we probe with December
// to get every month of the year that has data (future months come back as zero).
func (s *Service) MonthlyTrendForYear(year int) ([]MonthPoint, error) {
	return s.MonthlyTrend(fmt.Sprintf("%04d-12", year))
}

// RecentMonthlyTrend returns the trailing n months up to and including the current
// month, consolidated across all accounts. Because the trend endpoint only ever
// returns a single calendar year per query, we fetch each year the window spans
// (typically the current and previous year) and slice out the trailing months,
// filling gaps with zero so the series is always exactly n points in order.
func (s *Service) RecentMonthlyTrend(n int) ([]MonthPoint, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -(n - 1), 0)

	merged := make(map[string]MonthPoint)
	for y := start.Year(); y <= now.Year(); y++ {
		pts, err := s.MonthlyTrendForYear(y)
		if err != nil {
			return nil, err
		}
		for _, p := range pts {
			merged[p.Month] = p
		}
	}

	out := make([]MonthPoint, 0, n)
	for t := start; len(out) < n; t = t.AddDate(0, 1, 0) {
		key := t.Format("200601") // matches the endpoint's YYYYMM month keys
		if p, ok := merged[key]; ok {
			out = append(out, p)
		} else {
			out = append(out, MonthPoint{Month: key})
		}
	}
	return out, nil
}

// monthlyTrendForGroup fetches the trend for a single group.
//
// monthlyTrendForGroup fetches the trend for a single group, pinned to the
// calendar year of month. The endpoint returns that whole year (truncated at the
// searchDate month + 1), so probing with December yields the full year. Because
// the year is fixed by searchDate — not anchored to "now" — a past year's result
// is immutable and cacheTTL caches it permanently; the current year stays live.
func (s *Service) monthlyTrendForGroup(month, groupID string) ([]MonthPoint, error) {
	params := s.baseParams(month)
	params["groupIdForCustomer"] = groupID
	var r monthlyTrendResp
	if err := s.c.CallCached(epMonthlyTrend, "POST", params, &r, cacheTTL(month)); err != nil {
		return nil, err
	}
	out := make([]MonthPoint, 0, len(r.TrendData))
	for _, d := range r.TrendData {
		if d.UseDt == "" {
			continue
		}
		cost := d.Amount
		if cost == 0 {
			cost = d.AmountUsd
		}
		out = append(out, MonthPoint{Month: d.UseDt, Cost: cost, Estimated: d.EAmount})
	}
	return out, nil
}

// --- billing detail export (full month CSV) ---

const epMakeExcel = "/billingon/billingDetail/billingDetailMakeExcelAjax.do"

// ExportProgress is an optional callback invoked per group during export.
type ExportProgress func(group Group, rows int)

// ExportDetailCSV downloads the full line-item detail for a month as UTF-8 CSV
// bytes (a single header row followed by all data rows).
//
// The portal returns billing data per group, and which group holds the data
// depends on the month: a unified "all accounts" group in recent months, but
// historically one group per account (GROUP1, GROUP2, ...). The groups are
// mutually exclusive per month, so we iterate every group, download each
// non-empty CSV, and concatenate — keeping the header only once. The server
// emits CP949, which we transcode to UTF-8.
//
// If the service was created with an explicit group (-g), only that group is
// fetched.
func (s *Service) ExportDetailCSV(month string, progress ExportProgress) ([]byte, error) {
	var groups []Group
	if s.groupExplicit {
		groups = []Group{{GroupID: s.groupID, GroupName: ""}}
	} else {
		gs, err := s.Groups(s.customerIdx)
		if err != nil {
			return nil, err
		}
		groups = gs
	}

	// Download each group concurrently (bounded), preserving group order in the
	// merged output. A month's data lives in exactly one set of groups; empty
	// ones return just a header and are skipped.
	type result struct {
		header []byte
		body   []byte
		err    error
	}
	results := make([]result, len(groups))
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, g := range groups {
		wg.Add(1)
		go func(i int, g Group) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, err := s.downloadGroupCSV(month, g.GroupID)
			if err != nil {
				results[i] = result{err: err}
				return
			}
			utf8, err := decodeCP949(raw)
			if err != nil {
				results[i] = result{err: fmt.Errorf("group %s: %w", g.GroupID, err)}
				return
			}
			h, body := splitCSVHeader(utf8)
			results[i] = result{header: h, body: body}
		}(i, g)
	}
	wg.Wait()

	var header []byte
	var rows [][]byte
	for i, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		if progress != nil {
			progress(groups[i], countLines(r.body))
		}
		if len(r.body) == 0 {
			continue
		}
		if header == nil {
			header = r.header
		}
		rows = append(rows, r.body)
	}

	if header == nil {
		return nil, nil // no data for this month
	}
	out := make([]byte, 0, len(header)+1)
	out = append(out, header...)
	out = append(out, '\n')
	for _, r := range rows {
		out = append(out, r...)
		out = append(out, '\n')
	}
	return out, nil
}

// ExportInvoiceDetailCSV downloads the full invoice-basis line-item detail for
// the month as UTF-8 CSV. Unlike ExportDetailCSV (usage basis, per-group), this
// uses the invoice endpoint which takes the account list directly and returns
// the whole group at once — the amounts reconcile with `cost invoice` (the
// actual billed figures, after discounts).
//
// accountNos limits the export to specific accounts; empty means all accounts
// under the resolved group. The server accepts empty product/region filters to
// mean "all" (verified), so we don't enumerate them.
func (s *Service) ExportInvoiceDetailCSV(month string, accountNos []string) ([]byte, error) {
	if len(accountNos) == 0 {
		accts, err := s.AccountBreakdown(month)
		if err != nil {
			return nil, err
		}
		for _, a := range accts {
			accountNos = append(accountNos, a.AccountNo)
		}
	}
	form := url.Values{
		"excelMenu":          {"invoiceDetail"},
		"customerIdx":        {s.customerIdx},
		"companyName":        {s.companyName},
		"arrAccountIdStr":    {strings.Join(accountNos, ",")},
		"arrProductNameStr":  {""}, // empty = all products
		"arrRegionStr":       {""}, // empty = all regions
		"groupIdForCustomer": {s.groupID},
		"groupNmForCustomer": {s.companyName},
		"searchDate":         {monthCompact(month)},
		"partnerIdx":         {s.partnerIdx},
		"sort":               {"costUsd"},
		"sortBy":             {"desc"},
		"searchValue":        {""},
	}
	raw, err := s.c.DownloadFormCached(epInvoiceExcel, form, cacheTTL(month))
	if err != nil {
		return nil, err
	}
	return decodeCP949(raw)
}

func (s *Service) downloadGroupCSV(month, groupID string) ([]byte, error) {
	form := url.Values{
		"pageFlag":           {"billingDetail"},
		"menu":               {"dailyDetail"},
		"customerIdx":        {s.customerIdx},
		"companyName":        {s.companyName},
		"groupIdForCustomer": {groupID},
		"searchDate":         {monthHyphen(month)},
		"accountId":          {""},
		"rdoPartner":         {"undefined"},
		"sort":               {"useDtd"},
		"sortBy":             {"desc"},
		"searchValue":        {""},
	}
	return s.c.DownloadFormCached(epMakeExcel, form, cacheTTL(month))
}

// decodeCP949 transcodes the server's CP949 (EUC-KR) CSV bytes to UTF-8.
func decodeCP949(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return b, nil
	}
	dec := korean.EUCKR.NewDecoder()
	out, _, err := transform.Bytes(dec, b)
	return out, err
}

// splitCSVHeader separates the first line (header) from the remaining rows,
// trimming a trailing newline. Either part may be empty.
func splitCSVHeader(b []byte) (header, body []byte) {
	b = bytes.TrimRight(b, "\r\n")
	if len(b) == 0 {
		return nil, nil
	}
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return bytes.TrimRight(b[:i], "\r"), b[i+1:]
	}
	return b, nil // header only, no data rows
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return bytes.Count(b, []byte{'\n'}) + 1
}

// --- available months ---

type usageDatesResp struct {
	DataList []struct {
		UseDt     string `json:"useDt"`
		InvoiceDt string `json:"invoiceDt"`
	} `json:"dataList"`
}

// AvailableMonths returns the months with billing data (newest first).
func (s *Service) AvailableMonths() ([]string, error) {
	var r usageDatesResp
	if err := s.c.Call(epUsageDates, "GET", map[string]string{
		"customerIdx": s.customerIdx, "groupIdForCustomer": s.groupID,
	}, &r); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(r.DataList))
	for _, d := range r.DataList {
		out = append(out, d.UseDt)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

// --- helpers ---

func splitAccountID(id string) (no, alias string) {
	if i := strings.Index(id, "|"); i >= 0 {
		return id[:i], id[i+1:]
	}
	return id, ""
}

// parseMoney parses a server-formatted amount like "1,510.42" into a float.
// Returns 0 for empty/unparseable input.
func parseMoney(s string) float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func sortByCostDesc(items []LineItem) {
	sort.Slice(items, func(i, j int) bool { return items[i].Cost > items[j].Cost })
}

func validCustomers(in []Customer) []Customer {
	out := in[:0]
	for _, c := range in {
		if c.CustomerIdx != "" {
			out = append(out, c)
		}
	}
	return out
}

func validGroups(in []Group) []Group {
	out := in[:0]
	for _, g := range in {
		if g.GroupID != "" {
			out = append(out, g)
		}
	}
	return out
}

// pickDefaultGroup returns the consolidated group (name == company) when present.
func pickDefaultGroup(gs []Group) Group {
	if len(gs) == 0 {
		return Group{}
	}
	// Prefer the shortest group name (the unified parent over its "...1"/"...2" children).
	best := gs[0]
	for _, g := range gs[1:] {
		if len(g.GroupName) < len(best.GroupName) {
			best = g
		}
	}
	return best
}
