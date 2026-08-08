package crawler

import (
	"io/fs"
	// "fmt"
	"path/filepath"
)

type FSCrawler struct {
	BaseDir string
	Files []string
}

func NewFSCrawler(baseDir string) *FSCrawler {
	return &FSCrawler{
		BaseDir: baseDir,
		Files: []string{},
	}
}

func (c *FSCrawler) Crawl() error {
	return filepath.WalkDir(c.BaseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

	  // && filepath.Ext(path) == ".json"
		if !d.IsDir() {
			// fmt.Printf("%v \n", path)
			c.Files = append(c.Files, path)
		}
		return nil
	})
}

func (c *FSCrawler) GetCrawledEntries() []string {
	return c.Files
}
