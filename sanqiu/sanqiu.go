package sanqiu

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"

	"github.com/bibliolater/bookhunter/pkg/log"
	"github.com/bibliolater/bookhunter/pkg/progress"
	"github.com/bibliolater/bookhunter/pkg/rename"
	"github.com/bibliolater/bookhunter/pkg/spider"
)

// DefaultWebsite is the website for sanqiu book.
const DefaultWebsite = "https://www.sanqiu.cc"

var (
	bookIDRe = regexp.MustCompile(".*?/(\\d+?).html")
)

type downloader struct {
	config   *spider.Config
	progress *progress.Progress
	client   *spider.Client
	wait     *sync.WaitGroup
}

func NewDownloader(config *spider.Config) *downloader {
	// Create common http client.
	client := spider.NewClient(config)

	// Get last book ID
	last, err := latestBookID(client, config)
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("Find the last book ID: %d", last)

	// Create book storage.
	storageFile := path.Join(config.DownloadPath, config.ProgressFile)
	p, err := progress.NewProgress(int64(config.InitialBookID), last, storageFile)
	if err != nil {
		log.Fatal(err)
	}

	return &downloader{
		config:   config,
		progress: p,
		client:   client,
		wait:     new(sync.WaitGroup),
	}
}

// latestBookID will return the last available book ID.
func latestBookID(client *spider.Client, config *spider.Config) (int64, error) {
	resp, err := client.R().
		SetDoNotParseResponse(true).
		Get(config.Website)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.RawBody().Close() }()
	doc, err := goquery.NewDocumentFromReader(resp.RawBody())
	if err != nil {
		return 0, err
	}

	lastID := -1

	// Find all the links is case of the website master changed the theme.
	doc.Find("a").Each(func(i int, selection *goquery.Selection) {
		link, exists := selection.Attr("href")
		if exists {
			match := bookIDRe.FindStringSubmatch(link)
			// This is a book link.
			if len(match) > 0 {
				id, _ := strconv.Atoi(match[1])
				if id > lastID {
					lastID = id
				}
			}
		}
	})

	return int64(lastID), nil
}

// Fork a running instance.
func (d *downloader) Fork() {
	d.wait.Add(1)
	go d.download()
}

// Join will wait all the running instance be finished.
func (d *downloader) Join() {
	d.wait.Wait()
}

// download would start download books from given website.
func (d *downloader) download() {
	defer d.wait.Done()

	bookID := d.progress.AcquireBookID()
	log.Infof("Start to download book from %d.", bookID)

	// Try to acquire book ID from storage.
	for ; bookID != progress.NoBookToDownload; bookID = d.progress.AcquireBookID() {
		// Acquire book metadata from website.
		metadata := d.bookMetadata(bookID)
		if metadata == nil {
			log.Warnf("[%d/%d] Book with ID %d is not exist on target website.", bookID, d.progress.Size(), bookID)
			d.downloadedBook(bookID)
			continue
		}

		var links []string
		var err error
		enableAliYunDl := len(spider.AliyunConfig.RefreshToken) > 0
		if link, ok := metadata.Links[ALIYUN]; ok && enableAliYunDl {
			// Download books from aliyun drive
			links, err = spider.ResolveAliYunDrive(d.client, link.Url, link.Code, d.config.Formats...)
		} else if link, ok := metadata.Links[TELECOM]; ok {
			// Download books from telecom
			links, err = spider.ResolveTelecom(d.client, link.Url, link.Code, d.config.Formats...)
		} else {
			log.Warnf("[%d/%d] Book with ID %d don't have telecom link, skip.", bookID, d.progress.Size(), bookID)
		}

		if err != nil {
			log.Fatal(err)
		}
		if len(links) == 0 {
			log.Warnf("[%d/%d] No downloadable links found, this resource could be banned.", bookID, d.progress.Size())
		}

		for _, l := range links {
			err := d.downloadBook(metadata, l)
			if err != nil {
				log.Fatal(err)
			}
		}

		// Finished the book download.
		d.downloadedBook(bookID)
	}
}

// downloadBook would download the book to saving path.
func (d *downloader) downloadBook(meta *BookMeta, link string) error {
	save := func(filename string, contentLength int64, data io.ReadCloser) error {
		defer func() { _ = data.Close() }()
		// Generate file name.
		format, ok := spider.Extension(link)
		if !ok {
			format, _ = spider.Extension(filename)
		}
		newFilename := strconv.FormatInt(meta.Id, 10) + "." + strings.ToLower(format)
		if !d.config.Rename && filename != "" {
			newFilename = filename
		}
		// Remove illegal characters. Ref: https://en.wikipedia.org/wiki/Filename#Reserved_characters_and_words
		newFilename = rename.EscapeFilename(newFilename)
		// Generate the file path.
		file := filepath.Join(d.config.DownloadPath, newFilename)
		// Remove the exist file.
		if _, err := os.Stat(file); err == nil {
			if err := os.Remove(file); err != nil {
				return err
			}
		}
		// Create file writer.
		writer, err := os.Create(file)
		if err != nil {
			return err
		}
		defer func() { _ = writer.Close() }()
		// Add download progress
		bar := log.NewProgressBar(meta.Id, d.progress.Size(), format+" "+meta.Title, contentLength)
		// Write file content
		_, err = io.Copy(io.MultiWriter(writer, bar), data)
		if err != nil {
			return err
		}
		return nil
	}

	err := d.client.Download(link, save)
	if err != nil {
		return fmt.Errorf("download faild: %s", err)
	}
	return nil
}

// downloadedBook would record the download statue into storage.
func (d *downloader) downloadedBook(bookID int64) {
	if err := d.progress.SaveBookID(bookID); err != nil {
		log.Fatal(err)
	}
}
