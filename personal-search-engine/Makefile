all: main search

run: main
	./main

search:
	go run cmd/search/cli.go swot

crawler:
	go build -o cmd/crawl/main cmd/crawl/cli.go cmd/crawl/filetype.go cmd/crawl/notion_client.go

crawl:
	./cmd/crawl/main notion

main: clean
	go build -o main main.go

clean:
	rm -f ./main
