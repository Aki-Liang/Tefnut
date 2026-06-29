package store

type NodeType int

const (
	NodeComic NodeType = 1
	NodeDir   NodeType = 2
)

const (
	CoverNone   = 0
	CoverReady  = 1
	CoverFailed = 2
)

type Node struct {
	ID          int64
	ParentID    int64
	Name        string
	Path        string
	Type        NodeType
	PageCount   int
	CoverStatus int
	Author      string
	Rating           int
	ReadingDirection string
	DisplayMode      string
	Size             int64
	MTime       int64
	CreatedAt   int64
	UpdatedAt   int64
}

type Tag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type TagCount struct {
	Tag
	Count int `json:"count"`
}
