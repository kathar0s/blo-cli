package cmd

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/kathar0s/blo-cli/internal/billing"
	"github.com/kathar0s/blo-cli/internal/output"
	"github.com/spf13/cobra"
)

func newCostCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cost",
		Short: "비용 조회 (요약/월별/서비스별/계정별)",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return validateMonthFlag()
		},
	}
	c.PersistentFlags().StringVarP(&flagMonth, "month", "m", "", "조회 월 YYYY-MM (기본: 이번 달)")
	c.PersistentFlags().StringVarP(&flagGroup, "group", "g", "", "그룹 ID (기본: 통합 그룹 자동 선택)")
	c.AddCommand(newSummaryCmd(), newMonthlyCmd(), newServiceCmd(), newAccountCmd(), newInvoiceCmd(), newExportCmd())
	return c
}

func newInvoiceCmd() *cobra.Command {
	var (
		accounts []string
		service  string
		byAcct   bool
		bySvc    bool
	)
	c := &cobra.Command{
		Use:   "invoice",
		Short: "실제 청구금액 — 계정×서비스별 (인보이스 기준)",
		Long: "인보이스(청구서) 기준의 실제 청구금액을 계정별·서비스별로 보여줍니다.\n" +
			"이 값이 실제 청구액입니다(cost account/summary 의 사용액과 다를 수 있음).\n" +
			"--account 로 특정 계정만, --service 로 특정 서비스만(부분 일치),\n" +
			"--by-account 로 계정별 합계, --by-service 로 서비스별 합계를 볼 수 있습니다.\n" +
			"\n드릴다운 예: 서비스가 비싼 원인을 계정별로 좁히기\n" +
			"  blo cost invoice --by-service                              # RDS가 비싸네\n" +
			"  blo cost invoice --service RDS                             # 어느 계정인지 (계정별 내림차순)\n" +
			"  blo cost export --account <계정> -o rds.csv                # 그 계정 리소스별 상세",
		Args: func(cmd *cobra.Command, args []string) error {
			if byAcct && bySvc {
				return fmt.Errorf("--by-account 와 --by-service 는 함께 쓸 수 없습니다")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := newService()
			if err != nil {
				return err
			}
			items, err := svc.Invoice(defaultMonth(), splitAccounts(accounts))
			if err != nil {
				return err
			}

			// --service: 서비스명 부분 일치 필터 (대소문자 무시, 약칭 지원).
			// "RDS" 같은 약칭은 풀네임 키워드로 변환해 매칭한다. 특정 서비스만 볼 때는
			// 전체 할인/크레딧(조정) 행을 빼고 그 서비스의 청구액만 본다 — 할인은 포털
			// 전체에 적용되는 것이라 한 서비스에 귀속시키면 수치가 왜곡된다.
			if service != "" {
				needle := serviceAlias(service)
				filtered := items[:0]
				matched := false
				for _, it := range items {
					if !isAdjustment(it.Service) && strings.Contains(strings.ToLower(it.Service), needle) {
						filtered = append(filtered, it)
						matched = true
					}
				}
				if !matched {
					return fmt.Errorf("'%s' 와(과) 일치하는 서비스가 없습니다. `blo cost invoice --by-service` 로 서비스명을 확인하세요", service)
				}
				// 서버는 finalInvoiceList 를 계정 단위로 묶어 주므로, 한 서비스로
				// 필터링하면 계정 순서를 따라가 비용순이 아니다. 청구액 내림차순으로
				// 정렬해 "어느 계정이 비싼가"가 위에서 바로 보이게 한다.
				sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].CostUSD > filtered[j].CostUSD })
				items = filtered
			}

			if outFormat() == output.JSON {
				return output.JSONValue(output.Stdout, items)
			}

			// 할인/크레딧 행은 별도 조정으로 분리한다.
			// gross(할인 전 제품 합) + adjust(할인·크레딧) = net(실청구액).
			var gross, adjust float64
			lines := items[:0]
			for _, it := range items {
				if isAdjustment(it.Service) {
					adjust += it.CostUSD
					continue
				}
				gross += it.CostUSD
				lines = append(lines, it)
			}
			items = lines
			net := gross + adjust

			var rows output.Rows
			if byAcct {
				// 계정별 합계로 집계 (비용 내림차순)
				type agg struct {
					no    string
					alias string
					cost  float64
				}
				m := map[string]*agg{}
				for _, it := range items {
					a, ok := m[it.AccountNo]
					if !ok {
						a = &agg{no: it.AccountNo, alias: it.Alias}
						m[it.AccountNo] = a
					}
					a.cost += it.CostUSD
				}
				accs := make([]*agg, 0, len(m))
				for _, a := range m {
					accs = append(accs, a)
				}
				sort.Slice(accs, func(i, j int) bool { return accs[i].cost > accs[j].cost })
				rows.Header = []string{"계정번호", "별칭", "청구액(USD)"}
				for _, a := range accs {
					rows.Data = append(rows.Data, []string{a.no, a.alias, fmtCost(a.cost, outFormat())})
				}
			} else if bySvc {
				// 서비스별 합계로 집계 (비용 내림차순)
				m := map[string]float64{}
				for _, it := range items {
					m[it.Service] += it.CostUSD
				}
				type sc struct {
					name string
					cost float64
				}
				agg := make([]sc, 0, len(m))
				for k, v := range m {
					agg = append(agg, sc{k, v})
				}
				sort.Slice(agg, func(i, j int) bool { return agg[i].cost > agg[j].cost })
				rows.Header = []string{"서비스", "청구액(USD)"}
				for _, s := range agg {
					rows.Data = append(rows.Data, []string{s.name, fmtCost(s.cost, outFormat())})
				}
			} else {
				rows.Header = []string{"계정번호", "별칭", "서비스", "청구액(USD)"}
				for _, it := range items {
					rows.Data = append(rows.Data, []string{it.AccountNo, it.Alias, it.Service, fmtCost(it.CostUSD, outFormat())})
				}
			}
			if err := rows.Render(output.Stdout, outFormat()); err != nil {
				return err
			}
			if outFormat() == output.Table {
				fmt.Printf("\n제품 합계(할인 전): %s USD\n", output.Money(gross))
				if adjust != 0 {
					fmt.Printf("할인·크레딧:        %s USD\n", output.Money(adjust))
				}
				fmt.Printf("실청구액:           %s USD\n", output.Money(net))
			}
			return nil
		},
	}
	c.Flags().StringSliceVar(&accounts, "account", nil, "특정 AWS 계정 번호만 필터 (콤마 구분 또는 반복 지정)")
	c.Flags().StringVar(&service, "service", "", "특정 서비스만 필터 (약칭 가능: RDS, EC2, S3 등)")
	c.Flags().BoolVar(&byAcct, "by-account", false, "계정별 합계만 표시 (서비스 분해 생략)")
	c.Flags().BoolVar(&bySvc, "by-service", false, "서비스별 합계만 표시 (계정 분해 생략)")
	addOutputFlags(c)
	return c
}

// padRow builds a table row whose first cell is label and last cell is value,
// with empty middle cells, for a header of width n.
func padRow(n int, label, value string) []string {
	row := make([]string, n)
	row[0] = label
	for i := 1; i < n-1; i++ {
		row[i] = ""
	}
	if n >= 2 {
		row[n-1] = value
	}
	return row
}

func newExportCmd() *cobra.Command {
	var (
		outPath  string
		usage    bool
		accounts []string
	)
	c := &cobra.Command{
		Use:   "export",
		Short: "해당 월의 전체 상세 항목을 CSV로 내보내기 (실청구 기준)",
		Long: "한 달치 전체 라인아이템(계정/리소스/리전/제품/사용량/비용)을 UTF-8 CSV로 받습니다.\n" +
			"기본은 인보이스(실청구) 기준이라 합계가 `cost invoice` 와 일치합니다.\n" +
			"--account 로 특정 계정만, --usage 로 사용액(청구 전) 기준으로 받을 수 있습니다.\n" +
			"-o 생략 시 표준출력으로 내보냅니다.",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := newService()
			if err != nil {
				return err
			}
			month := defaultMonth()
			toFile := outPath != ""

			var csv []byte
			if usage {
				// 사용액 기준(기존 billingDetail, 그룹 순회 병합). 보존된 경로.
				var progress billing.ExportProgress
				if toFile {
					progress = func(g billing.Group, rows int) {
						if rows > 0 {
							name := g.GroupName
							if name == "" {
								name = g.GroupID
							}
							fmt.Fprintf(os.Stderr, "  %s: %d행\n", name, rows)
						}
					}
				}
				csv, err = svc.ExportDetailCSV(month, progress)
			} else {
				// 실청구 기준(invoiceDetail). 합계가 cost invoice 와 일치.
				if toFile {
					fmt.Fprintln(os.Stderr, "  인보이스 상세 내려받는 중...")
				}
				csv, err = svc.ExportInvoiceDetailCSV(month, splitAccounts(accounts))
			}
			if err != nil {
				return err
			}
			if len(csv) == 0 {
				return fmt.Errorf("%s 데이터가 없습니다", month)
			}

			if !toFile {
				_, err := os.Stdout.Write(csv)
				return err
			}
			if err := os.WriteFile(outPath, csv, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "저장 완료: %s (%d bytes)\n", outPath, len(csv))
			return nil
		},
	}
	c.Flags().StringVarP(&outPath, "out", "o", "", "저장할 파일 경로 (생략 시 표준출력)")
	c.Flags().BoolVar(&usage, "usage", false, "사용액(청구 전) 기준으로 내보내기 (기본은 실청구 기준)")
	c.Flags().StringSliceVar(&accounts, "account", nil, "특정 AWS 계정만 필터 (실청구 기준에서만, 콤마 구분)")
	return c
}

func newSummaryCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "summary",
		Short: "총비용 요약 (당월/전월/예상 + 증감률)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := newService()
			if err != nil {
				return err
			}
			month := defaultMonth()
			s, err := svc.CostSummary(month)
			if err != nil {
				return err
			}
			if outFormat() == output.JSON {
				return output.JSONValue(output.Stdout, s)
			}
			rows := output.Rows{
				Header: []string{"항목", "기간", "비용(USD)"},
				Data: [][]string{
					{"당월", rng(s.CurrentFrom, s.CurrentTo), output.Money(s.CurrentCost)},
					{"전월", rng(s.PreviousFrom, s.PreviousTo), output.Money(s.PreviousCost)},
					{"당월 예상", month, output.Money(s.EstimatedCost)},
				},
			}
			if err := rows.Render(output.Stdout, outFormat()); err != nil {
				return err
			}
			if outFormat() == output.Table {
				fmt.Printf("\n전월 대비: %+.1f%%\n", s.DeltaPct())
			}
			return nil
		},
	}
	addOutputFlags(c)
	return c
}

func newMonthlyCmd() *cobra.Command {
	var year int
	c := &cobra.Command{
		Use:   "monthly",
		Short: "월별 비용 추이 (기본: 최근 12개월, -y 로 특정 연도)",
		Long: "전체 계정을 통합한 월별 비용 추이를 보여줍니다.\n" +
			"기본은 이번 달까지의 최근 12개월이며, -y/--year 로 특정 연도(1~12월)를 볼 수 있습니다.\n\n" +
			"예시:\n" +
			"  blo cost monthly            # 최근 12개월\n" +
			"  blo cost monthly -y 2025    # 2025년 1~12월",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagMonth != "" {
				return fmt.Errorf("monthly 는 연도 단위입니다. 월(-m) 대신 연도(-y)를 쓰세요. 예: blo cost monthly -y 2025")
			}
			svc, _, err := newService()
			if err != nil {
				return err
			}
			var pts []billing.MonthPoint
			if year > 0 {
				pts, err = svc.MonthlyTrendForYear(year)
			} else {
				pts, err = svc.RecentMonthlyTrend(12)
			}
			if err != nil {
				return err
			}
			if outFormat() == output.JSON {
				return output.JSONValue(output.Stdout, pts)
			}
			rows := output.Rows{Header: []string{"월", "비용(USD)", "전월대비"}}
			for i, p := range pts {
				delta := ""
				if i > 0 && pts[i-1].Cost != 0 {
					delta = fmt.Sprintf("%+.1f%%", (p.Cost-pts[i-1].Cost)/pts[i-1].Cost*100)
				}
				rows.Data = append(rows.Data, []string{p.Month, fmtCost(p.Cost, outFormat()), delta})
			}
			return rows.Render(output.Stdout, outFormat())
		},
	}
	c.Flags().IntVarP(&year, "year", "y", 0, "조회 연도 YYYY (기본: 최근 12개월)")
	addOutputFlags(c)
	return c
}

func newServiceCmd() *cobra.Command {
	var (
		top      int
		accounts []string
	)
	c := &cobra.Command{
		Use:   "service",
		Short: "서비스(Product)별 비용 분해",
		Long: "서비스(Product)별 비용을 분해합니다(사용액 기준).\n" +
			"--account 로 특정 AWS 계정만 골라 볼 수 있습니다(여러 번 지정하거나 콤마로 구분).\n" +
			"\n" +
			"비용 기준 안내:\n" +
			" - 필터 없는 cost service / cost account = 사용액(그 달 사용분).\n" +
			" - --account 를 붙이면 인보이스(실청구) 기준으로 전환됩니다(cost invoice 와 동일 값).\n" +
			" - 실제 청구금액을 계정×서비스로 보려면 `blo cost invoice` 를 쓰세요.",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := newService()
			if err != nil {
				return err
			}
			month := defaultMonth()

			// --account 가 주어지면 계정 필터 가능한 Cost Explorer 엔드포인트를 쓴다.
			var items []billing.LineItem
			accountNos := splitAccounts(accounts)
			if len(accountNos) > 0 {
				items, err = svc.ServiceBreakdownByAccount(month, accountNos)
			} else {
				items, err = svc.ServiceBreakdown(month)
			}
			if err != nil {
				return err
			}
			if top > 0 && len(items) > top {
				items = items[:top]
			}
			if outFormat() == output.JSON {
				return output.JSONValue(output.Stdout, items)
			}
			rows := output.Rows{Header: []string{"서비스", "비용(USD)"}}
			var total float64
			for _, it := range items {
				rows.Data = append(rows.Data, []string{it.Name, fmtCost(it.Cost, outFormat())})
				total += it.Cost
			}
			if outFormat() == output.Table {
				rows.Data = append(rows.Data, []string{"합계", output.Money(total)})
			}
			if err := rows.Render(output.Stdout, outFormat()); err != nil {
				return err
			}
			if outFormat() == output.Table && len(accountNos) > 0 {
				fmt.Printf("\n대상 계정: %s\n", strings.Join(accountNos, ", "))
				fmt.Println("주: --account 는 인보이스(실청구) 기준입니다. 계정×서비스 청구내역은 `cost invoice` 와 동일합니다.")
			}
			return nil
		},
	}
	c.Flags().IntVar(&top, "top", 0, "상위 N개만 표시")
	c.Flags().StringSliceVar(&accounts, "account", nil, "특정 AWS 계정 번호만 필터 (콤마 구분 또는 반복 지정)")
	addOutputFlags(c)
	return c
}

// serviceAliases maps common AWS service abbreviations to a lowercase keyword
// found in the portal's full product name, so `--service RDS` matches
// "Amazon Relational Database Service".
var serviceAliases = map[string]string{
	"rds":          "relational database",
	"ec2":          "elastic compute cloud",
	"s3":           "simple storage",
	"elb":          "elastic load balancing",
	"vpc":          "virtual private cloud",
	"cloudwatch":   "cloudwatch",
	"cw":           "cloudwatch",
	"lambda":       "lambda",
	"dynamodb":     "dynamodb",
	"ddb":          "dynamodb",
	"ecr":          "container registry",
	"ecs":          "elastic container service",
	"eks":          "kubernetes",
	"efs":          "elastic file system",
	"cloudfront":   "cloudfront",
	"cf":           "cloudfront",
	"route53":      "route 53",
	"r53":          "route 53",
	"sqs":          "simple queue",
	"ses":          "simple email",
	"sns":          "simple notification",
	"kms":          "key management",
	"msk":          "streaming for apache kafka",
	"redshift":     "redshift",
	"sagemaker":    "sagemaker",
	"glacier":      "glacier",
	"elasticache":  "elasticache",
	"waf":          "waf",
	"sp":           "savings plans",
	"datatransfer": "data transfer",
}

// serviceAlias resolves a user-supplied service term to a lowercase match
// keyword: a known abbreviation maps to its full-name keyword; otherwise the
// term is lowercased and matched as a substring of the product name.
func serviceAlias(term string) string {
	key := strings.ToLower(strings.TrimSpace(term))
	if full, ok := serviceAliases[key]; ok {
		return full
	}
	return key
}

// isAdjustment reports whether an invoice line is a discount/credit adjustment
// rather than a real product line. These are summed separately so users see
// gross (before discount) and the actual billed (net) amounts distinctly.
func isAdjustment(service string) bool {
	s := strings.ToLower(strings.TrimSpace(service))
	return s == "" || strings.Contains(s, "discount") || strings.Contains(s, "credit") || strings.Contains(s, "할인")
}

// splitAccounts trims and drops empties from the --account values.
func splitAccounts(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

func newAccountCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "account",
		Short: "AWS 계정별 비용 분해",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := newService()
			if err != nil {
				return err
			}
			accts, err := svc.AccountBreakdown(defaultMonth())
			if err != nil {
				return err
			}
			if outFormat() == output.JSON {
				return output.JSONValue(output.Stdout, accts)
			}
			rows := output.Rows{Header: []string{"계정번호", "별칭", "비용(USD)"}}
			var total float64
			for _, a := range accts {
				rows.Data = append(rows.Data, []string{a.AccountNo, a.Alias, fmtCost(a.Cost, outFormat())})
				total += a.Cost
			}
			if outFormat() == output.Table {
				rows.Data = append(rows.Data, []string{"합계", "", output.Money(total)})
			}
			return rows.Render(output.Stdout, outFormat())
		},
	}
	addOutputFlags(c)
	return c
}

// --- shared helpers ---

func rng(from, to string) string {
	if from == "" && to == "" {
		return ""
	}
	return from + " ~ " + to
}

func fmtCost(v float64, f output.Format) string {
	if f == output.CSV {
		return output.Num(v)
	}
	// 0이 아니지만 2자리에서 "0.00" 으로 반올림되는 소액은 0 처럼 보이지 않게
	// "< 0.01"(음수는 "> -0.01")로 표시한다. CSV/JSON 은 원본 수치를 유지한다.
	if v != 0 && math.Abs(v) < 0.005 {
		if v < 0 {
			return "> -0.01"
		}
		return "< 0.01"
	}
	return output.Money(v)
}
