package servefile

import (
	//"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	//"io"
	"io/ioutil"
)

func init() {
	http.HandleFunc("/", handler)
}

// The Json input file "big.json" will contain 0 -> N instances of the
// following datatype used to reconstruct a larger file from a number
// of 32 meg chunks (the max GAE will upload for one file).

var chunkMap map[string][]string = make(map[string][]string)

var chunkMapParsed bool = false

func parse_chunk_map() error {
	var err error

	if chunkMapParsed == true {
		return nil
	}

	bytes, err := ioutil.ReadFile("big.json")
	if err != nil {
		return err
	}

	//log.Fatal("read bytes:", string(bytes))

	err = json.Unmarshal(bytes, &chunkMap)

	if err != nil {
		return err
	}

	chunkMapParsed = true

	return nil
}

// This util method will read a chunk file from the chunks directory
// and append the data in the file to the result buffer.

func chunk_concat(w http.ResponseWriter, chunkFilename string) {
	chunkPath := fmt.Sprintf("%s/%s", "chunk", chunkFilename)

	// FIXME: impl as streaming read and write of known buffer size
	// instead of 32 meg read and write operations.

	bytes, err := ioutil.ReadFile(chunkPath)
	if err != nil {
		log.Fatal("error:", err)
	}
	_, err = w.Write(bytes)
	if err != nil {
		log.Fatal("error:", err)
	}

	return
}

// A big file must be handled in a special way since GAE allows a max
// file size of 32M

func handler(w http.ResponseWriter, r *http.Request) {
	err := parse_chunk_map()

	if err != nil {
		log.Fatal("error:", err)
	} else {
		// Determine which file is being requested then construct cached version
		// by collecting the chunks together into one big download.

		//str := fmt.Sprintf("Big %s\n", bigPtr.Filname)
		//fmt.Fprint(w, str)

		//    w.Header().Set("Content-Type", bigPtr.ContentType)
		//    w.Header().Set("Content-Encoding", "gzip")
		//    w.Header().Set("Cache-control", "public, max-age=864000")

		for bigFilename, chunkArr := range chunkMap {
			fmt.Printf("Big Filename %s\n", bigFilename)

			for _, chunkFilename := range chunkArr {
				fmt.Printf("chunk_concat \t%s\n", chunkFilename)

				chunk_concat(w, chunkFilename)
			}
		}
	}
}
