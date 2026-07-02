package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// defaultTemplate is seeded into the config path on first start when no file
// exists (e.g. a freshly mounted /config volume), so users get an editable,
// commented file. No scan: section — scan settings live in the database and
// are edited on the settings page.
const defaultTemplate = `# Tefnut 配置文件。修改后重启容器生效。
# 缓存上限也可在 Web 设置页修改；设置页保存过的值优先于本文件。
library:
  rootPath: /comics # 漫画库目录（容器内路径，docker 只读挂载）
dataDir: /data # 数据库/缩略图/缓存（需持久卷）
server:
  addr: ":8086"
thumbnail:
  width: 400 # 封面宽度（像素）
  pageWidth: 120 # 页缩略图宽度（像素）
  pagesMaxBytes: 512MiB # 页缩略图缓存上限；0 = 不限制
cache:
  maxBytes: 2GiB # 解压缓存上限；0 = 不限制
`

// LoadOrInit loads the config at path; when the file does not exist it first
// writes the commented default template there (creating parent dirs), so a
// freshly mounted config volume self-seeds an editable file.
func LoadOrInit(path string) (*Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("config: create dir for %s: %w（检查配置目录挂载与权限）", path, err)
		}
		if err := os.WriteFile(path, []byte(defaultTemplate), 0o644); err != nil {
			return nil, fmt.Errorf("config: write default %s: %w（检查配置目录挂载与权限）", path, err)
		}
	}
	return Load(path)
}
