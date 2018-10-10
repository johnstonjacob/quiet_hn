package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gophercises/quiet_hn/hn"
)

type cache struct {
	stories []item
	current bool
}

var (
	cacheA cache
	cacheB cache
	mux    sync.Mutex
)

func main() {
	// parse flags
	var port, numStories int
	flag.IntVar(&port, "port", 3000, "the port to start the web server on")
	flag.IntVar(&numStories, "num_stories", 30, "the number of top stories to display")
	flag.Parse()

	tpl := template.Must(template.ParseFiles("./index.gohtml"))

	http.HandleFunc("/", handler(numStories, tpl))

	// Start the server
	cacheA.current = true
	go rotateCache(numStories)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))

}

func handler(numStories int, tpl *template.Template) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		stories := currentCache()

		data := templateData{
			Stories: stories,
			Time:    time.Now().Sub(start),
		}

		err := tpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Failed to process the template", http.StatusInternalServerError)
			return
		}
	})
}

func getTopStories(numStories int) ([]item, error) {
	var client hn.Client
	ids, err := client.TopItems()
	if err != nil {
		return nil, err
	}

	var stories []item
	at := 0
	for len(stories) < numStories {
		need := (numStories - len(stories)) * 5 / 4
		stories = append(stories, getStories(ids[at:at+need])...)
		at += need
	}

	return stories[:numStories], nil
}

func getStories(ids []int) []item {
	type result struct {
		idx  int
		item item
		err  error
	}
	resultCh := make(chan result)
	for i := 0; i < len(ids); i++ {
		go func(id, i int) {
			var client hn.Client
			hnItem, err := client.GetItem(id)
			if err != nil {
				resultCh <- result{err: err}
				return
			}
			resultCh <- result{idx: i, item: parseHNItem(hnItem)}
		}(ids[i], i)

	}

	var results []result
	for i := 0; i < len(ids); i++ {
		result := <-resultCh
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].idx < results[j].idx
	})

	var stories []item
	for _, result := range results {
		if isStoryLink(result.item) {
			stories = append(stories, result.item)
		}
	}

	return stories

}

func rotateCache(numStories int) {
	mux.Lock()
	if cacheA.current {
		cacheB.updateCache(numStories)
	} else {
		cacheA.updateCache(numStories)
	}

	cacheA.current, cacheB.current = cacheB.current, cacheA.current

	mux.Unlock()
	time.Sleep(15 * time.Minute)
	rotateCache(numStories)
}

func (cache *cache) updateCache(numStories int) {
	cache.stories, _ = getTopStories(numStories)
}

func currentCache() []item {
	mux.Lock()
	defer mux.Unlock()

	if cacheA.current {
		return cacheA.stories
	}
	return cacheB.stories
}

func isStoryLink(item item) bool {
	return item.Type == "story" && item.URL != ""
}

func parseHNItem(hnItem hn.Item) item {
	ret := item{Item: hnItem}
	url, err := url.Parse(ret.URL)
	if err == nil {
		ret.Host = strings.TrimPrefix(url.Hostname(), "www.")
	}
	return ret
}

// item is the same as the hn.Item, but adds the Host field
type item struct {
	hn.Item
	Host string
}

type templateData struct {
	Stories []item
	Time    time.Duration
}
