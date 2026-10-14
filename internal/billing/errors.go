package billing

import "errors"

var (
	errNoCustomer = errors.New("billing: 접근 가능한 고객사가 없습니다")
	errNoGroup    = errors.New("billing: 고객사에 그룹이 없습니다")
)
