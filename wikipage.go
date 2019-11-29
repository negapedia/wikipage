// Package wikipage provides utility functions for retrieving informations about Wikipedia articles.
package wikipage

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/pkg/errors"
)

// WikiPage represents an article of Wikipedia.
type WikiPage struct {
	ID       uint32 `json:"pageid"`
	Title    string
	Abstract string `json:"Extract"`
}

// New loads or creates a RequestHandler for the specified language.
func New(lang string) (rh RequestHandler) {
	title2Query := func(title string, life float64) string {
		title = underscoreRule.Replace(title)
		baseURL := ""
		if life > 0.75 { //Default API
			baseURL = "https://%v.wikipedia.org/api/rest_v1/page/summary/%v?redirect=true"
			title = url.PathEscape(title)
		} else { //Fall back API
			baseURL = "https://%v.wikipedia.org/w/api.php?action=query&prop=extracts&exintro=&explaintext=&exchars=512&format=json&formatversion=2&redirects=&titles=%v"
			title = url.QueryEscape(title)
		}
		return fmt.Sprintf(baseURL, lang, title)
	}

	return RequestHandler{
		title2Query,
	}
}

var underscoreRule = strings.NewReplacer(" ", "_")

// RequestHandler is a hub from which is possible to retrieve informations about Wikipedia articles.
type RequestHandler struct {
	title2Query func(title string, life float64) (query string)
}

// From returns a WikiPage from an article Title. It's safe to use concurrently. Warning: in the worst case it can block for more than 48 hours. As such it's advised to setup a timeout with the context.
func (rh RequestHandler) From(ctx context.Context, title string) (p WikiPage, err error) {
	//Query for page
	mayMissingPage, err := pageFrom(ctx, rh.title2Query(title, 1))

	if err != nil { //Handle error gracefully
		deadlines := expDeadlines(ctx, 48*time.Hour) //Exponential backoff deadlines
		for i, deadline := range deadlines {
			if err == nil || ctx.Err() != nil {
				break
			}
			context, cancel := context.WithDeadline(ctx, deadline)
			<-context.Done()
			cancel() //Not needed, used just to make happy "go vet"
			mayMissingPage, err = pageFrom(ctx, rh.title2Query(title, float64(len(deadlines)-i)/float64(len(deadlines))))
		}
	}

	//Handle errors
	switch {
	case err == nil && mayMissingPage.Missing:
		err = errors.WithStack(pageNotFound{title})
	case err != nil:
		//Do nothing
	default:
		p = mayMissingPage.WikiPage
	}

	return
}

//Exponential backoff deadlines
func expDeadlines(ctx context.Context, maxDuration time.Duration) (deadlines []time.Time) {
	deadline, ok := ctx.Deadline()
	now := time.Now()
	if twoDaysFromNow := now.Add(48 * time.Hour); !ok || twoDaysFromNow.Before(deadline) {
		deadline = twoDaysFromNow
	}

	db := 10 * time.Second
	da := deadline.Sub(now) - db
	deadlines = make([]time.Time, 0, 32)
	for da > 250*time.Millisecond {
		deadline = deadline.Add(-db)
		deadlines = append(deadlines, deadline)

		//Exponential backoff: every time delay is halved (average)
		db = time.Duration(rand.Int63n(int64(da)))
		da -= db
	}

	//Reverse
	for left, right := 0, len(deadlines)-1; left < right; left, right = left+1, right-1 {
		deadlines[left], deadlines[right] = deadlines[right], deadlines[left]
	}

	return
}

var client = &http.Client{Timeout: 10 * time.Second}
var limiter = rate.NewLimiter(150, 1)

func pageFrom(ctx context.Context, query string) (p mayMissingPage, err error) {
	fail := func(e error) (mayMissingPage, error) {
		p, err = mayMissingPage{}, errors.Wrapf(e, "error with the following query: %v", query)
		return p, err
	}

	request, err := http.NewRequestWithContext(ctx, "GET", query, nil)
	if err != nil {
		return fail(err)
	}
	//Set User-Agent as per wikipedia API rules https://en.wikipedia.org/api/rest_v1/#/Page_content
	request.Header.Set("User-Agent", "[https://github.com/negapedia/wikipage]")

	//Respect rate limiter as per wikipedia API rules https://en.wikipedia.org/api/rest_v1/#/Page_content
	err = limiter.Wait(ctx)
	if err != nil {
		return fail(err)
	}

	resp, err := client.Do(request)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fail(err)
	}

	//Marshalling results for two different replies for queries
	data := struct {
		//Rest API standard
		Type string
		*mayMissingPage

		//Result for query API
		Query struct {
			Pages []mayMissingPage
		}
	}{mayMissingPage: &p}

	err = json.Unmarshal(body, &data)
	if err != nil {
		return fail(err)
	}

	//Convert data to the expected format
	for _, p := range data.Query.Pages {
		*data.mayMissingPage = p
	}
	if data.Type == "https://mediawiki.org/wiki/HyperSwitch/errors/not_found" || data.ID == 0 {
		data.mayMissingPage.Missing = true
	}
	return
}

type mayMissingPage struct {
	Missing bool
	WikiPage
}

type pageNotFound struct {
	title string
}

func (err pageNotFound) Error() string {
	return fmt.Sprintf("%v wasn't found", err.title)
}

// NotFound checks if current error was issued by a page not found, if so it returns page ID and sets "ok" true, otherwise "ok" is false.
func NotFound(err error) (title string, ok bool) {
	pnf, ok := errors.Cause(err).(pageNotFound)
	if ok {
		title = pnf.title
	}
	return
}
