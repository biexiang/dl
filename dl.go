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
	BlockSize int64 = 2 * 1024 * 1024
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
	Downloaded    int64
	FileName      string
	CNT           int
	Blocks        [][]int64
	Handlers      []*os.File
	WG            sync.WaitGroup
}

func getDL(url string, concurrent int) *DL {
	log.Println("Downloading: ", url)
	return &DL{
		URL: url,
		CNT: concurrent,
		HTTPClient: &http.Client{
			Timeout: 300 * time.Second, // 5 min 超时
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

	dl.parseFileName(resp.Header)

	//init dir
	_, e = os.Stat(FilePath)
	if e != nil {
		if e = os.MkdirAll(FilePath, 0777); e != nil {
			log.Panicln(e)
		}
	}
	if _, e = os.Stat(FilePath + "/" + dl.FileName); !os.IsNotExist(e) {
		log.Println("File Exists ", FilePath+"/"+dl.FileName)
		os.Exit(0)
	}

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
		//file, e := os.OpenFile(fileNameStr, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
		file, e := os.OpenFile(fileNameStr, os.O_RDWR|os.O_APPEND, 0666)
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
	var (
		sizePerBlock int64
		lastBlock    int64
	)
	cnt := int64(dl.CNT)
	sizePerBlock = int64(math.Floor(float64((dl.ContentLength / cnt))))
	if dl.ContentLength%cnt != 0 {
		lastBlock = dl.ContentLength % cnt
	}
	var start int64
	for i := 0; i < dl.CNT; i++ {
		block := []int64{
			start,
			start + sizePerBlock - 1,
		}
		dl.Blocks = append(dl.Blocks, block)
		start += sizePerBlock
	}

	if lastBlock != 0 {
		lastBlock := []int64{
			start,
			start + lastBlock,
		}
		dl.Blocks = append(dl.Blocks, lastBlock)
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
	shouldWrite := dl.Blocks[index][1] - dl.Blocks[index][0] + 1
	//log.Println("Starting download block no.", index)

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
		log.Println("Copy Err")
		goto ERR
	}
	if written == 0 {
		log.Println("Write 0 byte")
		goto ERR
	}

	if shouldWrite-written > 1 {
		log.Println("Write Less ", shouldWrite, written)
		goto ERR
	}
	// atomic.AddInt64(&dl.Downloaded, written)
	// log.Println("dl", atomic.LoadInt64(&dl.Downloaded))
	//log.Println("Finished no.", index, " block")
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
		//log.Println(tmpFile)
		src, e = os.Open(tmpFile)
		if e != nil {
			goto ERR
		}
		written, e = io.Copy(dst, src)
		if e != nil || written <= 0 {
			goto ERR
		}
		//TODO Written WRONG
		//log.Println("Merged no.", i)
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

// func (dl *DL) showProgress() {
// 	//time.Sleep(time.Second)
// 	// uiprogress.Start()
// 	// bar := uiprogress.AddBar(100)
// 	// bar.AppendCompleted()
// 	// bar.PrependElapsed()

// 	for dl.Downloaded <= dl.ContentLength {
// 		//percent := math.Floor((float64(dl.Downloaded) / float64(dl.ContentLength)) * 100)
// 		log.Println(atomic.LoadInt64(&dl.Downloaded), dl.ContentLength)
// 		// bar.Set(int(percent))

// 		time.Sleep(1 * time.Second)
// 	}
// 	//bar.Set(100)
// }

func main() {
	var (
		concurrent = 10
		link       = ""
		e          error
	)
	flag.StringVar(&link, "l", "", "which stuff you want to download ? Give Me Link ")
	flag.IntVar(&concurrent, "c", 10, "how many download concurrent in same time")
	flag.Parse()
	if link == "" {
		printUsage()
	}
	dl := getDL(link, concurrent)
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
