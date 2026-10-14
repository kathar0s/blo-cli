// Package cmd wires up the blo CLI commands.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/kathar0s/blo-cli/internal/auth"
	"github.com/kathar0s/blo-cli/internal/billing"
	"github.com/kathar0s/blo-cli/internal/cache"
	"github.com/kathar0s/blo-cli/internal/client"
	"github.com/kathar0s/blo-cli/internal/output"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "0.1.0-dev"

// global flags
var (
	flagJSON    bool
	flagCSV     bool
	flagMonth   string
	flagGroup   string
	flagRefresh bool
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "blo",
		Short:         "빌링온(Billing-On) AWS 비용을 조회·분석하는 CLI",
		Long:          "빌링온 포털의 데이터를 명령줄에서 조회하고 CSV/JSON으로 내보냅니다.\n로그인: blo auth login",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&flagRefresh, "refresh", false, "캐시를 무시하고 최신 데이터를 다시 조회 (과거 월 캐시 갱신)")
	root.AddCommand(newAuthCmd(), newCostCmd(), newGroupsCmd(), newMonthsCmd(), newCacheCmd())
	return root
}

// Execute runs the root command.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
}

// addOutputFlags registers --json/--csv on a command.
func addOutputFlags(c *cobra.Command) {
	c.Flags().BoolVar(&flagJSON, "json", false, "JSON으로 출력")
	c.Flags().BoolVar(&flagCSV, "csv", false, "CSV로 출력")
}

func outFormat() output.Format { return output.FromFlags(flagJSON, flagCSV) }

// defaultMonth returns flagMonth or the current YYYY-MM.
func defaultMonth() string {
	if flagMonth != "" {
		return flagMonth
	}
	return time.Now().Format("2006-01")
}

// reMonth matches a valid YYYY-MM (month 01–12).
var reMonth = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// validateMonthFlag checks that --month, when given, is a well-formed YYYY-MM.
// The portal silently returns empty data for malformed months, so we reject
// them up front rather than reporting a misleading "0원" result.
func validateMonthFlag() error {
	if flagMonth == "" {
		return nil
	}
	if !reMonth.MatchString(flagMonth) {
		return fmt.Errorf("잘못된 월 형식입니다: %q (예: 2026-01)", flagMonth)
	}
	return nil
}

// newService restores the client from the saved session and resolves the
// billing context. If the session has expired, it returns an error asking the
// user to run `blo auth login` again (it does not silently re-authenticate).
func newService() (*billing.Service, *auth.Session, error) {
	cache.ForceRefresh = flagRefresh
	sess, err := auth.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("로그인이 필요합니다. 먼저 `blo auth login` 을 실행하세요")
		}
		return nil, nil, err
	}
	c, err := client.Restore(sess)
	if err != nil {
		return nil, nil, err
	}
	svc := billing.New(c, sess.CustomerIdx, flagGroup)
	if err := svc.Resolve(); err != nil {
		if errors.Is(err, client.ErrSessionExpired) {
			return nil, nil, fmt.Errorf("세션이 만료되었습니다. `blo auth login` 을 다시 실행하세요")
		}
		return nil, nil, err
	}
	// Cache resolved customer for next time.
	if cur, _ := svc.Context(); cur != "" && cur != sess.CustomerIdx {
		sess.CustomerIdx = cur
		_ = auth.Save(sess)
	}
	return svc, sess, nil
}
