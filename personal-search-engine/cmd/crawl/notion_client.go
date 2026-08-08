package main

// import (
// 	"net/http"
// 	"io"
// 	"os"
// 	"fmt"
// )
//
//
// func GetSubBlocks(blockId string) {
//
// 	apiKey := os.Getenv("NOTION_SECRET_KEY")
//
// 	url := fmt.Sprintf("https://api.notion.com/v1/blocks/%s/children", blockId)
//
// 	req, _ := http.NewRequest("GET", url, nil)
//
// 	req.Header.Add("Notion-Version", API_VERSION)
// 	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", apiKey))
// 	req.Header.Add("Content-Type", "application/json")
//
// 	res, err := http.DefaultClient.Do(req)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
//
// 	defer res.Body.Close()
// 	body, _ := io.ReadAll(res.Body)
//
// 	fmt.Println(string(body))
//
// 	// return page
// }
//
