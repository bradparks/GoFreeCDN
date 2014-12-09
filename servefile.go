package servefile

import (
	//"encoding/base64"
	"encoding/json"
	//"fmt"
	"log"
	"net/http"
	//"io"
	"io/ioutil"
)

func init() {
	http.HandleFunc("/", handler)
	http.HandleFunc("/big", bighandler)
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Fatal("error")
	/*
	   w.Header().Set("Content-Type", "video/mp4")
	   // Cache for 10 days
	   w.Header().Set("Cache-control", "public, max-age=1000")

	   dataBase64 := "==="

	   sDec, err := base64.StdEncoding.DecodeString(dataBase64)

	   if err != nil {
	     log.Fatal("error:", err)
	   } else {
	     _, err = w.Write(sDec)
	     if err != nil {
	       log.Fatal("error:", err)
	     }
	   }
	*/
}

// The Json input file "big.json" will contain 0 -> N instances of the
// following datatype used to reconstruct a larger file from a number
// of 32 meg chunks (the max GAE will upload for one file).

type BigDataFile struct {
	Filname         string   `json:"Filname"`
	ContentType     string   `json:"Content-Type"`
	ContentEncoding string   `json:"Content-Encoding"`
	Chunks          []string `json:"Chunks"`
}

var _parsedBigData *BigDataFile = nil

func big_parse() (*BigDataFile, error) {
	var err error

	if _parsedBigData != nil {
		return _parsedBigData, nil
	}

	bytes, err := ioutil.ReadFile("big.json")
	if err != nil {
		return nil, err
	}

	//log.Fatal("read bytes:", string(bytes))

	_parsedBigData = new(BigDataFile)

	err = json.Unmarshal(bytes, _parsedBigData)

	if err != nil {
		return nil, err
	}

	return _parsedBigData, nil
}

// A big file must be handled in a special way since GAE allows a max
// file size of 32M

func bighandler(w http.ResponseWriter, r *http.Request) {
	bigPtr, err := big_parse()

	if err != nil {
		log.Fatal("error:", err)
	} else {
		// Determine which file is being requested then construct cached version
		// by collecting the chunks together into one big download.

		//str := fmt.Sprintf("Big %s\n", bigPtr.Filname)
		//fmt.Fprint(w, str)

    w.Header().Set("Content-Type", bigPtr.ContentType)
    w.Header().Set("Content-Encoding", "gzip")
    w.Header().Set("Cache-control", "public, max-age=864000")

		for _, chunkFilename := range bigPtr.Chunks {
			bytes, err := ioutil.ReadFile(chunkFilename)
			if err != nil {
				log.Fatal("error:", err)
			}
			_, err = w.Write(bytes)
			if err != nil {
				log.Fatal("error:", err)
			}
		}
	}
}
