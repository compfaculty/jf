package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"jf/internal/httpx"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	var (
		url      = flag.String("url", "", "URL to inspect (required)")
		selector = flag.String("selector", "", "CSS selector to extract (optional, can specify multiple)")
		saveFile = flag.String("save", "", "Save full HTML to file (optional)")
		textOnly = flag.Bool("text", false, "Extract text only (no HTML tags)")
	)
	flag.Parse()

	if *url == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -url <URL> [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -url https://example.com\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -url https://example.com -selector \".wpjb-text\"\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -url https://example.com -selector \".wpjb-col-35\" -text\n", os.Args[0])
		os.Exit(1)
	}

	ctx := context.Background()
	client := httpx.New(httpx.HttpClientConfig{
		Timeout: 30 * time.Second,
	})

	fmt.Printf("Fetching: %s\n", *url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to fetch URL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	if *saveFile != "" {
		if err := os.WriteFile(*saveFile, body, 0644); err != nil {
			log.Fatalf("Failed to save file: %v", err)
		}
		fmt.Printf("✓ Saved full HTML to: %s\n", *saveFile)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		log.Fatalf("Failed to parse HTML: %v", err)
	}

	// If specific selectors provided, extract them
	selectors := flag.Args()
	if *selector != "" {
		selectors = append(selectors, *selector)
	}

	if len(selectors) > 0 {
		fmt.Printf("\n=== Extracted Content ===\n\n")
		for i, sel := range selectors {
			if i > 0 {
				fmt.Printf("\n---\n\n")
			}
			fmt.Printf("Selector: %s\n", sel)
			fmt.Printf("Results:\n")
			found := false
			doc.Find(sel).Each(func(idx int, s *goquery.Selection) {
				found = true
				fmt.Printf("\n[Match %d]:\n", idx+1)
				if *textOnly {
					text := strings.TrimSpace(s.Text())
					fmt.Println(text)
				} else {
					html, err := s.Html()
					if err != nil {
						fmt.Printf("Error getting HTML: %v\n", err)
					} else {
						// Pretty print HTML with indentation
						fmt.Println(html)
					}
				}
			})
			if !found {
				fmt.Printf("  (no matches)\n")
			}
		}
	} else {
		// Default: show common job board selectors
		fmt.Printf("\n=== Common Selectors Check ===\n\n")

		checks := []struct {
			name     string
			selector string
		}{
			{"Location", ".wpjb-grid-col.wpjb-col-35"},
			{"Location (alt)", "div.wpjb-col-35"},
			{"Description", ".wpjb-text"},
			{"Job Structure", ".wpjb.wpjb-job.wpjb-page-single"},
			{"Header Title", ".wpjb-top-header-title"},
			{"Content", ".post-content"},
		}

		for _, check := range checks {
			fmt.Printf("%s (%s):\n", check.name, check.selector)
			count := doc.Find(check.selector).Length()
			if count > 0 {
				fmt.Printf("  ✓ Found %d match(es)\n", count)
				doc.Find(check.selector).First().Each(func(_ int, s *goquery.Selection) {
					text := strings.TrimSpace(s.Text())
					if len(text) > 200 {
						text = text[:200] + "..."
					}
					fmt.Printf("  Preview: %s\n", text)
				})
			} else {
				fmt.Printf("  ✗ Not found\n")
			}
			fmt.Println()
		}

		// Check for "Location of job:" text
		fmt.Printf("=== Searching for 'Location of job:' text ===\n")
		foundLocation := false
		doc.Find("*").Each(func(_ int, s *goquery.Selection) {
			text := s.Text()
			if strings.Contains(strings.ToLower(text), "location of job") {
				foundLocation = true
				tagName := goquery.NodeName(s)
				fmt.Printf("\nFound in <%s>:\n", tagName)
				fmt.Printf("  Full text: %s\n", strings.TrimSpace(text))
				html, _ := s.Html()
				if len(html) > 500 {
					html = html[:500] + "..."
				}
				fmt.Printf("  HTML: %s\n", html)
				// Get class/ID attributes
				if class, ok := s.Attr("class"); ok {
					fmt.Printf("  Class: %s\n", class)
				}
				if id, ok := s.Attr("id"); ok {
					fmt.Printf("  ID: %s\n", id)
				}
				return
			}
		})
		if !foundLocation {
			fmt.Printf("  ✗ Not found\n")
		}

		// Check for REQUIREMENTS
		fmt.Printf("\n=== Searching for 'REQUIREMENTS' text ===\n")
		foundReqs := false
		doc.Find("*").Each(func(_ int, s *goquery.Selection) {
			text := strings.ToUpper(strings.TrimSpace(s.Text()))
			if strings.HasPrefix(text, "REQUIREMENTS") {
				foundReqs = true
				tagName := goquery.NodeName(s)
				fmt.Printf("\nFound in <%s>:\n", tagName)
				// Get parent container to see context
				parent := s.Parent()
				if parent.Length() > 0 {
					fmt.Printf("  Parent: <%s", goquery.NodeName(parent))
					if class, ok := parent.Attr("class"); ok {
						fmt.Printf(" class=\"%s\"", class)
					}
					fmt.Printf(">\n")
				}
				// Extract text from this element and following siblings
				allText := strings.TrimSpace(s.Text())
				if len(allText) > 500 {
					allText = allText[:500] + "..."
				}
				fmt.Printf("  Text preview: %s\n", allText)
				return
			}
		})
		if !foundReqs {
			fmt.Printf("  ✗ Not found\n")
		}
	}

	fmt.Printf("\n=== Done ===\n")
}
