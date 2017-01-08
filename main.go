package main

import (
	"net/http"
	"html/template"
	"encoding/json"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"net/url"
	"encoding/xml"
	"io/ioutil"
	"github.com/codegangsta/negroni"
)

type Page struct {
	Name string
	DBStatus bool
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
	} `xml:"recommendataions>ddc>mostPopular"`
}


type SearchResult struct {
	Title string `xml:"title,attr"`
	Author string `xml:"author,attr"`
	Year string `xml:"hyr,attr"`
	ID string `xml:"owi,attr"`
}

func main(){

	templates := template.Must(template.ParseFiles("templates/index.html"))

	db, _ := sql.Open("sqlite3", "dev.db")
	
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request){
		p := Page{Name: "Gopher"}
		if name := r.FormValue("name"); name != "" {
			p.Name = name
		}
		p.DBStatus = db.Ping() == nil
		
		if err := templates.ExecuteTemplate(w, "index.html", p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)}
	})

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
		
	})

	mux.HandleFunc("/books/add", func(w http.ResponseWriter, r *http.Request){

		var book ClassifyBookResponse
		var err error

		if book,err = find(r.FormValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err = db.Ping(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = db.Exec("insert into books (pk, title, author, id, classification) values (?, ?, ?, ?, ?)",
			nil, book.BookData.Title, book.BookData.Author, book.BookData.ID, book.Classification.MostPopular)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	})

	n := negroni.Classic()
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



