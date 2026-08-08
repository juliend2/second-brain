package parser

import (
	"os"
	"encoding/xml"
	"fmt"
	"strings"
	"bytes"

	"github.com/gomutex/godocx"
	"github.com/ledongthuc/pdf"
)

func GetTextContent(filePath string) string {
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading the file: '%s' \n", filePath)
		return ""
	}
	return string(bytes)
}

func GetTextFromWordDoc(filePath string) (string, error) {
	rootDoc, err := godocx.OpenDocument(filePath)
	if err != nil {
		fmt.Printf("Error reading the docx %s \n", err)
		return "", err
	}
	xmlBytes, err := xml.Marshal(rootDoc)
	decoder := xml.NewDecoder(strings.NewReader(string(xmlBytes)))
	var result strings.Builder
	for {
		tok, err := decoder.Token()
		if err != nil {
			break // io.EOF when done
		}
		if charData, ok := tok.(xml.CharData); ok {
			result.Write(charData)
		}

	}
	return result.String(), nil
}

func GetTextFromPdf(filePath string) (string, error) {
	pdf.DebugOn = false //true

	f, r, err := pdf.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	b, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	buf.ReadFrom(b)
	content := buf.String()

	return content, nil
}
