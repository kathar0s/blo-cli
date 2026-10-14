package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/kathar0s/blo-cli/internal/auth"
	"github.com/kathar0s/blo-cli/internal/billing"
	"github.com/kathar0s/blo-cli/internal/client"
	"github.com/kathar0s/blo-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCmd() *cobra.Command {
	c := &cobra.Command{Use: "auth", Short: "로그인/로그아웃/상태 확인"}
	c.AddCommand(newLoginCmd(), newLogoutCmd(), newStatusCmd())
	return c
}

func newLoginCmd() *cobra.Command {
	var email string
	c := &cobra.Command{
		Use:   "login",
		Short: "빌링온에 로그인하고 세션을 저장합니다",
		Long: "이메일/비밀번호로 로그인합니다.\n" +
			"비밀번호는 저장하지 않고, 발급된 세션만 " + auth.CredentialsLocation() + " 에 저장합니다.\n" +
			"환경변수 " + config.EnvEmail + " / " + config.EnvPassword + " 로도 전달할 수 있습니다(CI/자동화용).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" {
				email = os.Getenv(config.EnvEmail)
			}
			if email == "" {
				var err error
				email, err = prompt("이메일: ")
				if err != nil {
					return err
				}
			}
			password := os.Getenv(config.EnvPassword)
			if password == "" {
				var err error
				password, err = promptPassword("비밀번호: ")
				if err != nil {
					return err
				}
			}
			email = strings.TrimSpace(email)
			if email == "" || password == "" {
				return fmt.Errorf("이메일과 비밀번호가 필요합니다")
			}

			cl, err := client.New()
			if err != nil {
				return err
			}
			fmt.Println("로그인 중...")
			sess, err := cl.Login(email, password)
			if err != nil {
				return err
			}

			// 발급된 세션으로 실제 데이터 조회가 되는지 검증한다.
			// (302 리다이렉트만으로는 세션이 실제 동작하는지 확신할 수 없음)
			svc := billing.New(cl, sess.CustomerIdx, "")
			if err := svc.Resolve(); err != nil {
				return fmt.Errorf("로그인은 됐으나 세션 검증에 실패했습니다: %w", err)
			}
			if cIdx, _ := svc.Context(); cIdx != "" {
				sess.CustomerIdx = cIdx
			}

			if err := auth.Save(sess); err != nil {
				return err
			}

			fmt.Printf("\n✓ 로그인 성공: %s\n", email)
			if cs, err := svc.Customers(); err == nil && len(cs) > 0 {
				fmt.Printf("  고객사: %s\n", cs[0].CompanyName)
			}
			if months, err := svc.AvailableMonths(); err == nil && len(months) > 0 {
				fmt.Printf("  조회 가능 기간: %s ~ %s (%d개월)\n", months[len(months)-1], months[0], len(months))
			}
			fmt.Printf("  세션 저장: %s\n", auth.CredentialsLocation())
			fmt.Println("\n시작하기:  blo cost summary")
			return nil
		},
	}
	c.Flags().StringVar(&email, "email", "", "로그인 이메일")
	return c
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "저장된 세션을 삭제합니다",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.Clear(); err != nil {
				return err
			}
			fmt.Println("로그아웃 완료: 저장된 세션을 삭제했습니다.")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "현재 로그인 상태를 확인합니다",
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, err := auth.Load()
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("로그인되어 있지 않습니다. `blo auth login` 을 실행하세요.")
					return nil
				}
				return err
			}
			fmt.Printf("로그인됨: %s\n", sess.Email)
			fmt.Printf("세션 저장: %s (%s)\n", auth.CredentialsLocation(), sess.SavedAt.Format("2006-01-02 15:04:05"))
			if sess.CustomerIdx != "" {
				fmt.Printf("고객사 IDX: %s\n", sess.CustomerIdx)
			}
			return nil
		},
	}
}

func prompt(label string) (string, error) {
	fmt.Print(label)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	return strings.TrimSpace(line), err
}

func promptPassword(label string) (string, error) {
	fmt.Print(label)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	return string(b), err
}
