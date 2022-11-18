package fetcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/bookstairs/bookhunter/internal/client"
	"github.com/bookstairs/bookhunter/internal/driver"
	"github.com/bookstairs/bookhunter/internal/log"
	"github.com/bookstairs/bookhunter/internal/wordpress"
)

func newTianlangService(config *Config) (service, error) {
	resolver := func(c *client.Client, id int64) (map[driver.Source]wordpress.ShareLink, error) {
		return tianlangLinkResolver(config, c, id)
	}

	if err := cleanStaleCookies(config); err != nil {
		return nil, err
	}

	return newWordpressService(config, resolver)
}

func cleanStaleCookies(config *Config) error {
	path, err := config.Config.ConfigPath()
	if err != nil {
		return err
	}
	_, err = os.Stat(filepath.Join(path, client.CookieFile))
	if err != nil {
		return nil
	}
	_ = os.Remove(filepath.Join(path, client.CookieFile))
	return nil
}

func tianlangLinkResolver(config *Config, c *client.Client, id int64) (map[driver.Source]wordpress.ShareLink, error) {
	resp, err := c.R().
		SetPathParam("id", strconv.FormatInt(id, 10)).
		SetFormData(map[string]string{
			"secret_key": config.Property("secretKey"),
			"Submit":     "提交",
		}).
		ForceContentType("application/x-www-form-urlencoded").
		Post("/{id}.html")
	if err != nil {
		return nil, err
	}

	content := resp.String()
	log.Debugf("Get website content for book %d\n\n%s", id, content)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return nil, err
	}

	// Find all the links.
	links := map[driver.Source]wordpress.ShareLink{}
	doc.Find(".secret-password-content > p").Each(func(i int, selection *goquery.Selection) {
		find := selection.Find("a")
		href, exists := find.Attr("href")
		if !exists {
			return
		}
		text := selection.Text()

		for linkType, name := range driveNamings {
			if strings.Contains(text, name) {
				href, err = extractTianLangLink(c, href)
				links[linkType] = wordpress.ShareLink{URL: href}
			}
		}
		if err != nil {
			return
		}

		for linkType, link := range links {
			name := driveNamings[linkType]
			if strings.Contains(text, name) {
				if match := tianlangPasscodeRe.FindStringSubmatch(text); len(match) == 2 {
					link.Code = match[1]
					links[linkType] = link
				}
			}
		}
	})

	return links, err
}

func extractTianLangLink(c *client.Client, url string) (string, error) {
	log.Debugf("Resolve the jump link for tianlang book: %s", url)

	response, err := c.R().Get(url)
	if err != nil {
		return "", err
	}
	submatch := tianLangLinkRe.FindStringSubmatch(response.String())
	if len(submatch) < 2 {
		return "", fmt.Errorf("invalid tianlang share link: %s", url)
	}

	return submatch[1], nil
}
