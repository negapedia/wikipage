package wikipage

import (
	"context"
	"encoding/json"
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
	p, err := rh.From(context.Background(), pageID)
	switch {
	case err != nil:
		t.Error("New returns ", err)
	case p.ID != pageID:
		t.Error("New returns info for", p.ID, "expected for", pageID)
	case p.Title != title:
		t.Error("New returns info for", p.ID, "expected", title, "got", p.Title)
	}
	pageID = uint32(0)
	p, err = rh.From(context.Background(), pageID)
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
			wikipage, err := rh.From(context.Background(), pageID)
			wikipageCheck, ok := generatePage(pageID)
			switch {
			case err != nil && ok:
				t.Error("For", pageID, "expected", wikipageCheck, "got", err.Error())
			case err != nil: // && !ok:
				if _, IsNotFoundErr := NotFound(err); !IsNotFoundErr {
					t.Error("For", pageID, "expected", pageNotFound{pageID}.Error(), "got", err.Error())
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
		sPageIDs := r.URL.Query()["pageids"]

		var IDs []uint32
		if len(sPageIDs) > 0 {
			IDs = IDsFrom(sPageIDs[0])
		}

		pp := []mayMissingPage{}
		for _, ID := range IDs {
			p, ok := generatePage(ID)
			p.ID = ID
			pp = append(pp, mayMissingPage{p, !ok})
		}

		response := struct {
			Batchcomplete bool
			Query         struct {
				Pages []mayMissingPage
			}
		}{Batchcomplete: true}
		response.Query.Pages = pp

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

func IDsFrom(s string) (IDs []uint32) {
	for _, sID := range strings.Split(s, "|") {
		if ID, err := strconv.ParseUint(sID, 10, 32); err == nil {
			IDs = append(IDs, uint32(ID))
		}
	}
	return
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
