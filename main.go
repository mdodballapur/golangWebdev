package main

import (
	"net/http"
	"encoding/json"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"net/url"
	"encoding/xml"
	"io/ioutil"
	"github.com/urfave/negroni"
	"github.com/goincremental/negroni-sessions"
	"github.com/goincremental/negroni-sessions/cookiestore"
	"github.com/yosssi/ace"
	gmux "github.com/gorilla/mux"
	"log"
	"fmt"
	"strconv"
	"gopkg.in/gorp.v1"
	"errors"
)

type Book struct {
	PK int64 `db:"pk"`
	Title string `db:"title"`
	Author string	`db:"author"`
	Classification string `db:"classification"`
	ID string `db:"id"`
}

type Page struct {
	Books []Book
}

type ClassifyResponse struct {
	Results []SearchResult `xml:"works>work"`
}

type ClassifyBookResponse struct {
	BookData struct {
		Title string `xml:"title,attr"`
		Author string `xml:"author,attr"`
		ID string `xml:"owi,attr"`
	} `xml:"work"`

	Classification struct {
		MostPopular string `xml:"sfa,attr"`
	} `xml:"recommendations>ddc>mostPopular"`
}

type SearchResult struct {
	Title string `xml:"title,attr"`
	Author string `xml:"author,attr"`
	Year string `xml:"hyr,attr"`
	ID string `xml:"owi,attr"`
}


var db *sql.DB
var dbmap *gorp.DbMap


func initDB() error {
	var err error

	if db, err = sql.Open("sqlite3", "dev.db"); err != nil {
		return errors.New("Database connection failed to open")
	}

	dbmap = &gorp.DbMap{Db: db, Dialect: gorp.SqliteDialect{}}

	dbmap.AddTableWithName(Book{}, "books").SetKeys(true, "pk")
	dbmap.CreateTablesIfNotExists()

	return nil
}

func verifyDatabase(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {

	if db == nil {
		http.Error(w, "Database connection not open", http.StatusInternalServerError)
		return
	}
	if err := db.Ping(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next(w, r)
}

func getBookCollection(books *[]Book, sortCol string, w http.ResponseWriter) bool {
	if sortCol != "title" && sortCol != "author"  && sortCol != "classification" {
		sortCol = "pk"
	}

	if _, err := dbmap.Select(books, "select * from books order by " + sortCol); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}

	return true
}

func main(){
	//db, _ = sql.Open("sqlite3", "dev.db")

	mux := gmux.NewRouter()

	mux.HandleFunc("/books", func(w http.ResponseWriter, r *http.Request){
		var b []Book

		if !getBookCollection(&b, r.FormValue("sortBy"), w ) {
			return
		}

		sessions.GetSession(r).Set("sortBy", r.FormValue("sortBy"))

		if err := json.NewEncoder(w).Encode(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}).Methods("GET")

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request){
		template, err := ace.Load("templates/index", "", nil)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		var sortColumn string

		if sortBy := sessions.GetSession(r).Get("sortBy"); sortBy != nil {
			sortColumn = sortBy.(string)
		}

		p := Page{Books: []Book{}}

		if !getBookCollection(&p.Books, sortColumn, w){
			return
		}

		if err := template.Execute(w, p); err != nil {
		//if err := template.Execute(w, vl); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}).Methods("GET")

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request){
		var results []SearchResult
		var err error

		if results,err = search(r.FormValue("search")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		encoder := json.NewEncoder(w)
		if err = encoder.Encode(results); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}).Methods("POST")

	mux.HandleFunc("/books/add", func(w http.ResponseWriter, r *http.Request){
		var book ClassifyBookResponse
		var err error

		id := r.FormValue("id")
		log.Print("Adding id: ", id)

		if book,err = find(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}


		b := Book {
			PK: -1,
			Title: book.BookData.Title,
			Author: book.BookData.Author,
			Classification: book.Classification.MostPopular,
		}

		if err = dbmap.Insert(&b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err = json.NewEncoder(w).Encode(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}).Methods("PUT")

	mux.HandleFunc("/books/{pk}", func(w http.ResponseWriter, r *http.Request){
		pk, _ := strconv.ParseInt(gmux.Vars(r)["pk"], 10, 64)
		if _,err := dbmap.Delete(&Book{pk, "", "", "", ""}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}).Methods("Delete")

	if err := initDB(); err != nil {
		fmt.Println("%s", err)
		return
	}
	n := negroni.Classic()
	n.Use(negroni.NewLogger())
	n.Use(sessions.Sessions("go-for-web-dev", cookiestore.New([]byte("my-secret-123"))))
	n.Use(negroni.HandlerFunc(verifyDatabase))
	n.UseHandler(mux)
	n.Run(":8080")
}

func find (id string) (ClassifyBookResponse, error) {
	var c ClassifyBookResponse

	body, err := classifyAPI("http://classify.oclc.org/classify2/Classify?&summary=true&owi=" + url.QueryEscape(id))

	if err != nil {
		return ClassifyBookResponse{}, err
	}

	err = xml.Unmarshal(body, &c)
	if err != nil {
		log.Print("Unable to unmarshal data about book")
		return ClassifyBookResponse{}, err
	}
	return c, err
}

func search(query string) ([]SearchResult, error) {
	var c ClassifyResponse

	classifyUrl := "http://classify.oclc.org/classify2/Classify?&summary=true&title=" + url.QueryEscape(query)

	body, err := classifyAPI(classifyUrl)

	if err != nil {
		return []SearchResult{}, err
	}

	err = xml.Unmarshal(body, &c)
	return c.Results, err
}

func classifyAPI(classifyUrl string) ([]byte, error) {
	var resp *http.Response
	var err error

	if resp, err = http.Get(classifyUrl); err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}
