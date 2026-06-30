package store

import "strings"

// nodeColNames is the single source of truth for nodes columns and their order.
// scanNode, the Get/ListChildren SELECT, and the Search SELECT all derive from
// this list. To add a column: append it here, add the matching pointer in
// nodeScanTargets in the SAME position, and add a migration (Task 2). The
// startup assertion (assertNodeColumns) guards the count.
var nodeColNames = []string{
	"id", "parent_id", "name", "path", "type", "page_count", "cover_status",
	"author", "rating", "size", "mtime", "created_at", "updated_at",
	"reading_direction", "display_mode",
}

// nodeCols is the unprefixed, comma-joined column list (for single-table SELECTs).
var nodeCols = strings.Join(nodeColNames, ", ")

// nodeColsPrefixed returns the column list with each name prefixed by alias+"."
// (for joined queries, e.g. Search's "n." alias).
func nodeColsPrefixed(alias string) string {
	parts := make([]string, len(nodeColNames))
	for i, c := range nodeColNames {
		parts[i] = alias + "." + c
	}
	return strings.Join(parts, ", ")
}

// nodeScanTargets returns scan destinations for n in nodeColNames order.
func nodeScanTargets(n *Node) []any {
	return []any{
		&n.ID, &n.ParentID, &n.Name, &n.Path, &n.Type, &n.PageCount, &n.CoverStatus,
		&n.Author, &n.Rating, &n.Size, &n.MTime, &n.CreatedAt, &n.UpdatedAt,
		&n.ReadingDirection, &n.DisplayMode,
	}
}

func init() { assertNodeColumns() }

// assertNodeColumns fails fast at startup if the column list and the scan-target
// list have drifted out of sync.
func assertNodeColumns() {
	if got := len(nodeScanTargets(&Node{})); got != len(nodeColNames) {
		panic("store: nodeScanTargets length " + itoa(got) + " != nodeColNames length " + itoa(len(nodeColNames)))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
