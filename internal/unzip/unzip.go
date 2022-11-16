package unzip

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Encoding to use. Since this implements the encoding.Encoding
// interface from golang.org/x/text/encoding you can trivially
// change this out for any of the other implemented encoders,
// e.g. `traditionalchinese.Big5`, `charmap.Windows1252`,
// `korean.EUCKR`, etc.
var encoding = simplifiedchinese.GB18030

// Unzip - struct
type Unzip struct {
	Src  string
	Dest string
}

// New - Create a new Unzip.
func New(src string, dest string) Unzip {
	return Unzip{src, dest}
}

// Extract - Extract zip file.
func (uz Unzip) Extract() error {
	r, err := zip.OpenReader(uz.Src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	_ = os.MkdirAll(uz.Dest, 0755)

	for _, f := range r.File {
		err := uz.extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}

// Closure to address file descriptors issue with all the deferred Close() methods.
func (uz Unzip) extractAndWriteFile(f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() {
		if err := rc.Close(); err != nil {
			panic(err)
		}
	}()

	path, err := sanitizeArchivePath(uz.Dest, encodingFilename(f.Name))
	if err != nil {
		return err
	}

	if !strings.HasPrefix(path, filepath.Clean(uz.Dest)+string(os.PathSeparator)) {
		return fmt.Errorf("%s: illegal file path", path)
	}

	if f.FileInfo().IsDir() {
		_ = os.MkdirAll(path, f.Mode())
	} else {
		mode := f.FileHeader.Mode()
		if mode&os.ModeType == os.ModeSymlink {
			data, err := io.ReadAll(rc)
			if err != nil {
				return err
			}
			_ = writeSymbolicLink(path, string(data))
		} else {
			_ = os.MkdirAll(filepath.Dir(path), f.Mode())
			outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := outFile.Close(); err != nil {
					panic(err)
				}
			}()

			// G110: Potential DoS vulnerability via decompression bomb.
			for {
				_, err := io.CopyN(outFile, rc, 1024)
				if err != nil {
					if err == io.EOF {
						break
					}
					return err
				}
			}
		}
	}

	return nil
}

func writeSymbolicLink(filePath string, targetPath string) error {
	err := os.MkdirAll(filepath.Dir(filePath), 0755)
	if err != nil {
		return err
	}

	err = os.Symlink(targetPath, filePath)
	if err != nil {
		return err
	}

	return nil
}

// sanitizeArchivePath sanitize archive file pathing from "G305: Zip Slip vulnerability"
func sanitizeArchivePath(d, t string) (v string, err error) {
	v = filepath.Join(d, t)
	if strings.HasPrefix(v, filepath.Clean(d)) {
		return v, nil
	}

	return "", fmt.Errorf("%s: %s", "content filepath is tainted", t)
}

// encodingFilename will convert the GBK into UTF-8
func encodingFilename(name string) string {
	i := bytes.NewReader([]byte(name))
	decoder := transform.NewReader(i, encoding.NewDecoder())
	content, err := io.ReadAll(decoder)
	if err != nil {
		// Fallback to default UTF-8 encoding
		return name
	} else {
		return string(content)
	}
}
