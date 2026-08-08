package crawler

import (
	"errors"
	"encoding/json"
	"net/http"
	"io"
	"os"
	"fmt"
	"time"
)

type TitleItem struct {
    PlainText   string      `json:"plain_text"`
    Type        string      `json:"type"`
}

const CACHE_FILE_MASK = "cache/%s.json"

type Page struct {
	Id 					string `json:id`
	Url 				string `json:url`
	CreatedTime string `json:created_time`
	Properties struct {
		Title struct {
			ID    string      `json:"id"`
			Type  string      `json:"type"`
			Title []TitleItem `json:"title"` // Note: this is an array
		} `json:"title"`
	} `json:"properties"`
}
func (p *Page) GetTitle() string {
    items := p.Properties.Title.Title
    if len(items) == 0 {
        return ""
    }
    return items[0].PlainText
}

func getCachedBlock(blockID string) ([]byte, error) {
	data, err := os.ReadFile(fmt.Sprintf(CACHE_FILE_MASK, blockID))
	if err != nil {
		return []byte{}, errors.New("Error while opening the file. Maybe it does not exist.")
	}
	fmt.Printf("getCachedBlock %s \n", blockID)
	return data, nil
}

func cacheBlock(blockID string, data []byte) error {
	err := os.WriteFile(fmt.Sprintf(CACHE_FILE_MASK, blockID), data, 0666)
	return err
}

func NewNotionGetRequest(url, version, apiKey string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return req, err
	}
	req.Header.Add("Notion-Version", version)
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Add("Content-Type", "application/json")

	return req, nil
}

func GetNotionPage(pageID string) (Page, error) {
	apiKey := os.Getenv("NOTION_SECRET_KEY")
	if apiKey == "" {
		return Page{}, errors.New("API Key not provided.")
	}
	url := "https://api.notion.com/v1/pages/"+pageID
	req, _ := NewNotionGetRequest(url, "2025-09-03", apiKey)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var block Page
	errUnmarshal := json.Unmarshal(body, &block)

	if errUnmarshal != nil {
		fmt.Println(errUnmarshal)
	}

	return block, nil
}

func GetBlockMarkdown(pageID string) ([]byte, error) {
	// TODO: make the cache transient
	cacheKey := pageID + "-markdown"
	data, err := getCachedBlock(cacheKey)
	if err == nil {
		fmt.Println("prend la cache de markdown")
		return data, nil
	}

	fmt.Printf("Going to crawl markdown for %s ...\n", pageID)

	apiKey := os.Getenv("NOTION_SECRET_KEY")
	if apiKey == "" {
		return []byte{}, errors.New("API Key not provided.")
	}

	time.Sleep(500 * time.Millisecond)
	url := "https://api.notion.com/v1/pages/"+pageID+"/markdown"
	req, _ := NewNotionGetRequest(url, "2026-03-11", apiKey)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	cacheBlock(cacheKey, body)
	return body, nil
}

func GetMarkdown(pageID string) (string, error) {
	body, err := GetBlockMarkdown(pageID)
	if err != nil {
		panic(err)
	}

	var jsn map[string]any
	errUnmarshal := json.Unmarshal(body, &jsn)

	if errUnmarshal != nil {
		fmt.Println(errUnmarshal)
	}

	return jsn["markdown"].(string), nil
}

func GetBlockJSON(pageID string) ([]byte, error) {
	// TODO: make this cache transient
	// Maybe it's cached?
	cachedBytes, err := getCachedBlock(pageID)
	if err == nil && len(cachedBytes) > 0 {
		return cachedBytes, nil
	}

	// Not cached; get it:
	time.Sleep(400 * time.Millisecond) // politeness
	url := fmt.Sprintf("https://api.notion.com/v1/blocks/%s/children", pageID)
	req, _ := NewNotionGetRequest(url, "2026-03-11", os.Getenv("NOTION_SECRET_KEY"))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	cacheBlock(pageID, body)

	return body, nil
}

func GetChildPageIds(parentPageID string) ([]string, error) {
	var pageIds []string
	apiKey := os.Getenv("NOTION_SECRET_KEY")
	if apiKey == "" {
		return pageIds, errors.New("API Key not provided.")
	}

	body, _ := GetBlockJSON(parentPageID)
	var resp map[string]any
	errUnmarshal := json.Unmarshal(body, &resp)
	if errUnmarshal != nil {
		fmt.Println(errUnmarshal)
	}

	results, ok := resp["results"].([]any)
	if !ok {
		return pageIds, errors.New("No result in JSON response")
	}

	// get page ids
	for _, r := range results {
		result := r.(map[string]any)
		if result["type"] == "child_page" {
			pageIds = append(pageIds, result["id"].(string))
		}
	}

	return pageIds, nil
}

// Depth first search of Notion pages
func NotionPageSearch(accumulator *[]string, pageID *string) {
	if pageID == nil {
		return
	}
	fmt.Printf("%s \n", *pageID)
	*accumulator = append(*accumulator, *pageID)

	pageIDs, err := GetChildPageIds(*pageID)
	if err != nil {
		panic(err)
	}
	for _, child := range pageIDs {
		NotionPageSearch(accumulator, &child)
	}
}
