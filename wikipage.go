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
	queryBase := "https://%v.wikipedia.org/api/rest_v1/page/summary/%v?redirect=true"

	return RequestHandler{
		lang,
		queryBase,
	}
}

//Logger is used to log abnormal events while using wikipeadia extracts API.
var Logger = log.New(os.Stderr, "Wikipage", log.LstdFlags)

// RequestHandler is a hub from which is possible to retrieve informations about Wikipedia articles.
type RequestHandler struct {
	lang, queryBase string
}

// From returns a WikiPage from an article Title. It's safe to use concurrently. Warning: in the worst case if there are problems with the Wikipedia API it can block for more than 48 hours. As such it's advised to setup a timeout with the context.
func (rh RequestHandler) From(ctx context.Context, title string) (p WikiPage, err error) {
	query := fmt.Sprintf(rh.queryBase, rh.lang, url.PathEscape(underscoreRule.Replace(title)))

	typedPage, err := stubbornPageFrom(ctx, query)

	switch {
	case err == nil && typedPage.Type == "https://mediawiki.org/wiki/HyperSwitch/errors/not_found":
		err = errors.WithStack(pageNotFound{typedPage.Title})
		fallthrough
	case err != nil:
		Logger.Println("Final ", err)
	default:
		p = typedPage.WikiPage
	}

	return
}

var underscoreRule = strings.NewReplacer(" ", "_")

func stubbornPageFrom(ctx context.Context, query string) (p typedPage, err error) {
	for t := 250 * time.Millisecond; ctx.Err() == nil && t < 48*time.Hour; t *= 2 { //Exponential backoff
		p, err = pageFrom(ctx, query)
		switch {
		case err == nil:
			return
		case t < time.Minute:
			//go on
		case t > time.Hour:
			Logger.Println("While querying wikipedia API, occurred", err, "Next retry within", t)
			fallthrough
		default:
			client.CloseIdleConnections() //Soft client connection reset
		}

		//Sleep of max lenght t, cancellable by outer context ctx
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(rand.Int63n(int64(t))))
		<-timeoutCtx.Done()
		cancel()
	}

	p, err = pageFrom(ctx, query)
	return
}

var client = &http.Client{Timeout: 10 * time.Second}
var limiter = rate.NewLimiter(150, 1)

func pageFrom(ctx context.Context, query string) (p typedPage, err error) {
	fail := func(e error) (typedPage, error) {
		p, err = typedPage{}, errors.Wrapf(e, "error with the following query: %v", query)
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

	err = json.Unmarshal(body, &p)
	if err != nil {
		return fail(err)
	}

	return
}

type typedPage struct {
	Type string
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
