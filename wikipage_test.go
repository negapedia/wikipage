package wikipage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
)

const (
	N       = 1000
	TIMEOUT = 29 * time.Second
)

func TestUnit(t *testing.T) {
	pageID, title := uint32(12), "Anarchism"
	rh := New("en")
	p, err := rh.From(context.Background(), title)
	switch {
	case err != nil:
		t.Error("From returns ", err)
	case p.ID != pageID:
		t.Error("From returns info for", p.ID, "expected", pageID)
	case p.Title != title:
		t.Error("From returns info for", p.Title, "expected", title)
	}
	p, err = rh.From(context.Background(), "0test1test2test3")
	_, ok := NotFound(err)
	switch {
	case err == nil:
		t.Error("From should return an error, instead it returns", p)
	case !ok:
		t.Error("From returns an unexpected error", err)
	}
}

func TestPageFrom(t *testing.T) {
	rh := New("en")
	ctx, cancel := context.WithTimeout(context.Background(), TIMEOUT)
	defer cancel()
	for _, life := range []float64{1., 0.} {
		pageID, title := uint32(12), "Anarchism"
		p, err := pageFrom(ctx, rh.title2Query(title, life))
		rh.From(ctx, title)
		switch {
		case err != nil:
			t.Error("pageFrom(", title, ",", life, ") returns ", err)
		case p.ID != pageID:
			t.Error("pageFrom(", title, ",", life, ") returns info for", p.ID, "expected", pageID)
		case p.Title != title:
			t.Error("ageFrom(", title, ",", life, ") returns info for", p.Title)
		}
		p, err = pageFrom(ctx, rh.title2Query("0test1test2test3", life))
		if !p.Missing {
			t.Error("pageFrom(", title, ",", life, ") returns should be flagged as missing, instead it returns", p)
		}
	}
}
func TestFrom(t *testing.T) {
	rh := New("mytest")
	rh.title2Query = func(title string, life float64) string {
		return "http://" + address + "?pageids=" + title
	}

	donePageID := make(chan uint32)
	for pageID := uint32(1); pageID < N; pageID++ {
		go func(pageID uint32) {
			defer func() {
				donePageID <- pageID
			}()
			ctx, cancel := context.WithTimeout(context.Background(), TIMEOUT)
			defer cancel()
			wikipage, err := rh.From(ctx, fmt.Sprint(pageID))
			wikipageCheck, ok := generatePage(pageID)
			switch {
			case err != nil && ok:
				t.Error("For", pageID, "expected", wikipageCheck, "got", err.Error())
			case err != nil: // && !ok:
				if _, IsNotFoundErr := NotFound(err); !IsNotFoundErr {
					t.Error("For", pageID, "expected", pageNotFound{fmt.Sprint(pageID)}.Error(), "got", err.Error())
				}
			default:
				if wikipage != wikipageCheck {
					t.Error("For", pageID, "expected", wikipage, "got", wikipageCheck)
				}
			}
		}(pageID)
	}

	ctime := time.After(TIMEOUT)
	IDSet := roaring.NewBitmap()
	IDSet.AddRange(0, N)
	for i := 1; i < N; i++ {
		select {
		case ID := <-donePageID:
			IDSet.Remove(ID)
		case <-ctime:
			t.Error(
				"Timing out after", TIMEOUT,
				"With the following IDs", IDSet.String(),
			)
			return
		}
	}
}

const address = ":8080"

func TestMain(m *testing.M) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if rand.Intn(100) == 0 { //Add in some random errors
			return
		}

		ID, err := strconv.ParseUint(r.URL.Query()["pageids"][0], 10, 32)
		if err != nil {
			panic(err)
		}

		p, ok := generatePage(uint32(ID))
		response := mayMissingPage{Missing: false, WikiPage: p}
		if !ok {
			response.Missing = true
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			panic(err)
		}
	})
	go func() {
		err := http.ListenAndServe(address, nil)
		if err != nil {
			panic(err)
		}
	}()

	os.Exit(m.Run())
}

func generatePage(pageID uint32) (wp WikiPage, ok bool) {
	if pageID%7 == 0 {
		return
	}
	return WikiPage{pageID, stringFrom(int(pageID) / 10), stringFrom(int(pageID))}, true
}

func stringFrom(ID int) string {
	return "ba" + strings.Repeat("na", ID)
}
