package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// FOR TEST dl.net/download/fs.mkv

var (
	//ErrResource .
	ErrResource = errors.New("Download Resource has Problem")
	//ErrParseMedia .
	ErrParseMedia = errors.New("Parse Media Fail")
)

const (
	//BlockSize .
	BlockSize int64 = 100 * 1024 * 1024
	//FilePath .
	FilePath string = "./tmp"
)

//DL .
type DL struct {
	URL           string
	AcceptRange   bool
	Start         time.Time
	Elapse        time.Duration
	HTTPClient    *http.Client
	ContentLength int64
	FileName      string
	CNT           int
	Blocks        [][]int64
	Handlers      []*os.File
	WG            sync.WaitGroup
}

func getDL(url string) *DL {
	log.Println(url)
	return &DL{
		URL: url,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func printUsage() {
	fmt.Printf("Download Client Usage: \n")
	flag.PrintDefaults()
	os.Exit(2)
}

func (dl *DL) init() (e error) {
	var (
		req  *http.Request
		resp *http.Response
	)
	dl.Start = time.Now()
	req, e = http.NewRequest("HEAD", dl.URL, nil)
	if e != nil {
		return e
	}
	resp, e = dl.HTTPClient.Do(req)
	if e != nil {
		return e
	}
	// Check Status
	if resp.StatusCode != 200 {
		return ErrResource
	}
	dl.ContentLength = resp.ContentLength
	if resp.Header.Get("Accept-Ranges") != "" {
		dl.AcceptRange = true
	}

	//init dir
	_, e = os.Stat(FilePath)
	if e != nil {
		if e = os.MkdirAll(FilePath, 0777); e != nil {
			log.Panicln(e)
		}
	}

	dl.parseFileName(resp.Header)
	if dl.AcceptRange {
		dl.parseBlocks()
		dl.parseFileHandle()
	}
	//log.Printf("%v\n", dl)
	return nil
}

func (dl *DL) parseFileHandle() {
	for i := 0; i < len(dl.Blocks); i++ {
		suffix := fmt.Sprintf("%d_%d", dl.Blocks[i][0], dl.Blocks[i][1])
		fileNameStr := FilePath + "/" + dl.FileName + "_" + suffix
		file, e := os.OpenFile(fileNameStr, os.O_RDONLY|os.O_APPEND, 0777)
		if e == nil {
			fs, e := file.Stat()
			if e == nil {
				dl.Blocks[i][0] += fs.Size()
			} else {
				//TODO
				log.Panicln(e)
			}
		} else {
			file, e = os.Create(fileNameStr)
			if e != nil {
				log.Panicln(e)
			}
		}
		dl.Handlers = append(dl.Handlers, file)
	}
}

func (dl *DL) parseBlocks() {
	dl.CNT = int(math.Ceil(float64(dl.ContentLength / BlockSize)))
	var start int64
	for i := 0; i < dl.CNT; i++ {
		if i != dl.CNT-1 {
			block := []int64{
				start,
				start + BlockSize - 1,
			}
			dl.Blocks = append(dl.Blocks, block)
			start += BlockSize
		}
	}
}

func (dl *DL) parseFileName(h http.Header) {
	var (
		MediaParams map[string]string
	)
	_, MediaParams, _ = mime.ParseMediaType(h.Get("Content-Disposition"))
	if filename, ok := MediaParams["filename"]; ok {
		dl.FileName = filename
		return
	}

	// Parse Url
	slice := strings.Split(dl.URL, "/")
	dl.FileName = slice[len(slice)-1]
}

func (dl *DL) download(index int) {
	var (
		e       error
		req     *http.Request
		resp    *http.Response
		written int64
	)

	defer dl.WG.Done()
	if dl.Blocks[index][0] >= dl.Blocks[index][1] {
		//Already Finish this block
		return
	}

	//TODO 关闭文件
	defer dl.Handlers[index].Close()

	_range := fmt.Sprintf("%d-%d", dl.Blocks[index][0], dl.Blocks[index][1])
	req, e = http.NewRequest("GET", dl.URL, nil)
	req.Header.Set("Range", "bytes="+_range)
	if e != nil {
		goto ERR
	}

	resp, e = dl.HTTPClient.Do(req)
	if e != nil {
		goto ERR
	}
	defer resp.Body.Close()
	written, e = io.Copy(dl.Handlers[index], resp.Body)
	if e != nil {
		goto ERR
	}
	if (dl.Blocks[index][1] - dl.Blocks[index][0] + 1) != written {
		goto ERR
	}
	log.Println("Finished no.", index, " block")
	return

ERR:
	log.Panicln("Download Panic", e)

}

func (dl *DL) mergeFile() {
	var (
		dst     *os.File
		src     *os.File
		e       error
		written int64
	)

	dst, e = os.Create(FilePath + "/" + dl.FileName)
	if e != nil {
		goto ERR
	}
	for i := 0; i < len(dl.Handlers); i++ {
		tmpFile := dl.Handlers[i].Name()
		log.Println(tmpFile)
		src, e = os.Open(tmpFile)
		if e != nil {
			goto ERR
		}
		written, e = io.Copy(dst, src)
		if e != nil || written <= 0 {
			goto ERR
		}
		//TODO Written WRONG
		log.Println("Merged no.", i)
	}
	return
ERR:
	log.Panicln("MergeFile Fail ", e)

}

func (dl *DL) ending() {
	var e error
	for i := 0; i < len(dl.Handlers); i++ {
		e = os.Remove(dl.Handlers[i].Name())
		if e != nil {
			goto ERR
		}
	}
	dl.Elapse = time.Now().Sub(dl.Start)
	return
ERR:
	log.Panicln("End Fail", e)
}

func main() {
	var (
		link = ""
		e    error
	)
	flag.StringVar(&link, "link", "", "which stuff you want to download ? Give Me Link ")
	flag.Parse()
	if link == "" {
		printUsage()
	}
	dl := getDL(link)
	e = dl.init()
	if e != nil {
		goto ERR
	}

	for i := 0; i < len(dl.Blocks); i++ {
		dl.WG.Add(1)
		go dl.download(i)
	}
	dl.WG.Wait()
	//Merge
	dl.mergeFile()
	//end
	dl.ending()
	log.Println("Download Succeed ", dl.Elapse.String())

	os.Exit(0)
ERR:
	log.Println(e)
}
