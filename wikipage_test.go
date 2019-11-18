package wikipage

import (
	"context"
	"encoding/json"
	"fmt"
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
	TIMEOUT = 10 * time.Second
)

func TestUnit(t *testing.T) {
	pageID, title := uint32(12), "Anarchism"
	rh := New("en")
	p, err := rh.From(context.Background(), title)
	switch {
	case err != nil:
		t.Error("New returns ", err)
	case p.ID != pageID:
		t.Error("New returns info for", p.ID, "expected for", pageID)
	case p.Title != title:
		t.Error("New returns info for", p.ID, "expected", title, "got", p.Title)
	}
	p, err = rh.From(context.Background(), "0test1test2test3")
	_, ok := NotFound(err)
	switch {
	case err == nil:
		t.Error("New should return an error, instead it returns", p)
	case !ok:
		t.Error("New returns an unexpected error", err)
	}
}
func TestFrom(t *testing.T) {
	rh := New("mytest")
	rh.queryBase = "http://" + address + "?lang=%v&pageids=%v"
	donePageID := make(chan uint32)
	for pageID := uint32(0); pageID < N; pageID++ {
		go func(pageID uint32) {
			defer func() {
				donePageID <- pageID
			}()
			wikipage, err := rh.From(context.Background(), fmt.Sprint(pageID))
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
	for i := 0; i < N; i++ {
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
		ID, err := strconv.ParseUint(r.URL.Query()["pageids"][0], 10, 32)
		if err != nil {
			panic(err)
		}

		p, ok := generatePage(uint32(ID))
		response := typedPage{Type: "standard", WikiPage: p}
		if !ok {
			response.Type = "https://mediawiki.org/wiki/HyperSwitch/errors/not_found"
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
