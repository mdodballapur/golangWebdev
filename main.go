package main

import (
	"net/http"
	"encoding/json"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"net/url"
	"encoding/xml"
	"io/ioutil"
	"github.com/codegangsta/negroni"
	"github.com/yosssi/ace"
	gmux "github.com/gorilla/mux"
	"log"
	"fmt"

)

type Book struct {
	PK int
	Title string
	Author string
	Classification string
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

func verifyDatabase(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {

	if err := db.Ping(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next(w, r)
}

func main(){

	db, _ = sql.Open("sqlite3", "dev.db")
	mux := gmux.NewRouter()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request){
		template, err := ace.Load("templates/index", "", nil)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		p := Page{Books: []Book{}}

		rows, _ := db.Query("select pk, title, author, classification from books")

		for rows.Next() {
			var b Book
			rows.Scan(&b.PK, &b.Title, &b.Author, &b.Classification)
			p.Books = append(p.Books, b)
		}

		if err := template.Execute(w, p); err != nil {
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

		row := fmt.Sprintf("insert into books (pk, title, author, id, classification) values (NULL, \"%s\", \"%s\", \"%s\", \"%s\")",
												book.BookData.Title, book.BookData.Author,book.BookData.ID,book.Classification.MostPopular)

		log.Print("inserting row: ", row)
		result, err := db.Exec(row)

		if err != nil {
			log.Print	("Unable to insert row into database")
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		pk, _ := result.LastInsertId()
		b := Book {
			PK: int(pk),
			Title: book.BookData.Title,
			Author: book.BookData.Author,
			Classification: book.Classification.MostPopular,
		}

		if err = json.NewEncoder(w).Encode(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}).Methods("PUT")

	mux.HandleFunc("/books/{pk}", func(w http.ResponseWriter, r *http.Request){
		if _,err := db.Exec("delete from books where pk = ?", gmux.Vars(r)["pk"]); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}).Methods("Delete")



	n := negroni.Classic()
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
