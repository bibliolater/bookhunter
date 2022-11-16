package driver

import (
	"errors"
	"io"

	"github.com/bookstairs/bookhunter/internal/client"
	"github.com/bookstairs/bookhunter/internal/driver/lanzou"
)

func newLanzouDriver(c *client.Config, _ map[string]string) (Driver, error) {
	drive, err := lanzou.NewDrive(c)
	if err != nil {
		return nil, err
	}

	return &lanzouDriver{driver: drive}, errors.New("we don't support lanzou currently")
}

type lanzouDriver struct {
	driver *lanzou.Drive
}

func (l *lanzouDriver) Source() Source {
	return LANZOU
}

func (l *lanzouDriver) Resolve(shareLink string, passcode string) ([]Share, error) {
	resp, err := l.driver.ResolveShareURL(shareLink, passcode)
	if err != nil {
		return nil, err
	}
	shareList := make([]Share, len(*resp))
	for i, item := range *resp {
		shareList[i] = Share{
			FileName: item.Name,
			URL:      item.URL,
		}
	}
	return shareList, err
}

func (l *lanzouDriver) Download(share Share) (io.ReadCloser, error) {
	return l.driver.DownloadFile(share.URL)
}
