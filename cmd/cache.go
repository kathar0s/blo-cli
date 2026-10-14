package cmd

import (
	"fmt"

	"github.com/kathar0s/blo-cli/internal/cache"
	"github.com/spf13/cobra"
)

func newCacheCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cache",
		Short: "로컬 응답 캐시 관리",
		Long: "blo 는 과거 월의 조회 결과를 로컬에 캐시합니다(이번 달은 캐시하지 않음).\n" +
			"지난달은 24시간, 그 이전 월은 영구히 캐시되며, 어떤 명령이든 --refresh 로\n" +
			"캐시를 무시하고 다시 조회할 수 있습니다.",
	}
	c.AddCommand(newCacheInfoCmd(), newCacheClearCmd())
	return c
}

func newCacheInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "캐시 위치와 사용량 표시",
		RunE: func(cmd *cobra.Command, args []string) error {
			n, size := cache.Stats()
			fmt.Printf("위치: %s\n", cache.Dir())
			fmt.Printf("항목: %d개, %.1f KB\n", n, float64(size)/1024)
			return nil
		},
	}
}

func newCacheClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "캐시된 모든 항목 삭제",
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := cache.Clear()
			if err != nil {
				return err
			}
			fmt.Printf("캐시 %d개 항목을 삭제했습니다.\n", n)
			return nil
		},
	}
}
