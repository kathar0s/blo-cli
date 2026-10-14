package cmd

import (
	"github.com/kathar0s/blo-cli/internal/output"
	"github.com/spf13/cobra"
)

func newGroupsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "groups",
		Short: "고객사의 그룹 목록을 조회합니다",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := newService()
			if err != nil {
				return err
			}
			cIdx, _ := svc.Context()
			groups, err := svc.Groups(cIdx)
			if err != nil {
				return err
			}
			if outFormat() == output.JSON {
				return output.JSONValue(output.Stdout, groups)
			}
			rows := output.Rows{Header: []string{"그룹 ID", "그룹명"}}
			for _, g := range groups {
				rows.Data = append(rows.Data, []string{g.GroupID, g.GroupName})
			}
			return rows.Render(output.Stdout, outFormat())
		},
	}
	addOutputFlags(c)
	return c
}

func newMonthsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "months",
		Short: "비용 데이터가 있는 월 목록을 조회합니다",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, _, err := newService()
			if err != nil {
				return err
			}
			months, err := svc.AvailableMonths()
			if err != nil {
				return err
			}
			if outFormat() == output.JSON {
				return output.JSONValue(output.Stdout, months)
			}
			rows := output.Rows{Header: []string{"사용월"}}
			for _, m := range months {
				rows.Data = append(rows.Data, []string{m})
			}
			return rows.Render(output.Stdout, outFormat())
		},
	}
	addOutputFlags(c)
	return c
}
