package main

import (
	"fmt"
	"os"
	"github.com/blevesearch/bleve"
)

const INDEX_FILE = "pse.bleve"

func main() {
	searchQuery := os.Args[1]

	index, err := bleve.Open(INDEX_FILE)

	// search for some text
	// keep this only for dev's feedback loop.
	query := bleve.NewMatchQuery(searchQuery)
	search := bleve.NewSearchRequest(query)
	searchResults, err := index.Search(search)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(searchResults)
}
