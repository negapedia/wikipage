// Package wikipage provides utility functions for retrieving informations about Wikipedia articles.
package wikipage

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
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
		if life > 0.8 { //Default
			baseURL = "https://%v.wikipedia.org/api/rest_v1/page/summary/%v?redirect=true"
			title = url.PathEscape(title)
		} else { //Backup
			baseURL = "https://%v.wikipedia.org/w/api.php?action=query&prop=extracts&exintro=&explaintext=&exchars=512&format=json&formatversion=2&redirects=&titles=%v"
			title = url.QueryEscape(title)
		}
		return fmt.Sprintf(baseURL, lang, title)
	}

	return RequestHandler{
		title2Query,
	}
}

//Logger is used to log abnormal events while using wikipeadia extracts API.
var Logger = log.New(os.Stderr, "Wikipage", log.LstdFlags)

// RequestHandler is a hub from which is possible to retrieve informations about Wikipedia articles.
type RequestHandler struct {
	title2Query func(title string, life float64) (query string)
}

// From returns a WikiPage from an article Title. It's safe to use concurrently. Warning: in the worst case if there are problems with the Wikipedia API it can block for more than 48 hours. As such it's advised to setup a timeout with the context.
func (rh RequestHandler) From(ctx context.Context, title string) (p WikiPage, err error) {
	//Explicitly calculate query life
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * 48 * time.Hour)
	}
	maxDuration := float64(time.Now().Sub(deadline))
	queryLife := func() float64 {
		return float64(time.Now().Sub(deadline)) / float64(maxDuration)
	}

	//Query for page
	mayMissingPage, err := pageFrom(ctx, rh.title2Query(title, queryLife()))

	for t := 250 * time.Millisecond; err != nil && ctx.Err() == nil && t < 48*time.Hour; t *= 2 { //Exponential backoff
		switch {
		case t < time.Minute:
			//go on
		case t > time.Hour:
			Logger.Println("While querying wikipedia API, occurred", err, "- Next retry within", t)
			fallthrough
		default:
			client.CloseIdleConnections() //Soft client connection reset
		}

		//Sleep of max lenght t, cancellable by outer context ctx
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(rand.Int63n(int64(t))))
		<-timeoutCtx.Done()
		cancel()

		mayMissingPage, err = pageFrom(ctx, rh.title2Query(title, queryLife()))
	}

	//Handle errors
	switch {
	case err == nil && mayMissingPage.Missing:
		err = errors.WithStack(pageNotFound{mayMissingPage.Title})
		fallthrough
	case err != nil:
		Logger.Println("Fatal", err)
	default:
		p = mayMissingPage.WikiPage
	}

	return
}

var underscoreRule = strings.NewReplacer(" ", "_")
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
	return fmt.Sprintf("Page %v wasn't found", err.title)
}

// NotFound checks if current error was issued by a page not found, if so it returns page ID and sets "ok" true, otherwise "ok" is false.
func NotFound(err error) (title string, ok bool) {
	pnf, ok := errors.Cause(err).(pageNotFound)
	if ok {
		title = pnf.title
	}
	return
}
