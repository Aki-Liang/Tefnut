package entity

import (
	"Tefnut/common/defines"
	"context"
	"xorm.io/xorm"
)

type LibraryQuery struct {
	ParentId int `json:"parentId"`
	LastId   int `json:"lastId"`
	Limit    int `json:"limit"`
}

func (q *LibraryQuery) Process(ctx context.Context, session *xorm.Session) *xorm.Session {
	session.Where("parent_id = ? and id > ?", q.ParentId, q.LastId)
	if q.Limit != defines.NoLimit {
		session.Limit(q.Limit)
	}
	return session
}
