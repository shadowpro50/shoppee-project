package main

import (
	"context"
	"crypto/sha1"
	"fmt"
	"html/template"

	// "image/png"
	// "encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	// "github.com/joho/godotenv"
	uuid "github.com/satori/go.uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	// "gopkg.in/mgo.v2"
)

type item struct {
	Screenshot string `bson:"screenshot,omitempty"`
	Name       string `bson:"name,omitempty"`
	Price      string `bson:"price,omitempty"`
	Category   string `bson:"category,omitempty"`
	Seller     string `bson:"seller,omitempty"`
}

type user struct {
	Username string `bson:"username,omitempty"`
	Password string `bson:"password,omitempty"`
}

var tpl *template.Template
var dbUsers = map[string]user{}
var dbSessions = map[string]string{}

func init() {
	tpl = template.Must(template.ParseGlob("templates/*.html"))
}

func main() {
	ctx := context.Background()
	client := ConnectDB(ctx)
	defer client.Disconnect(ctx)
	http.HandleFunc("/", index)
	http.HandleFunc("/login", login)
	http.HandleFunc("/signup", signup)
	http.HandleFunc("/logout", logout)
	http.HandleFunc("/createListing", createListing)
	http.HandleFunc("/viewListing", viewListing)

	http.Handle("/public/", http.StripPrefix("/public", http.FileServer(http.Dir("./public"))))
	http.Handle("/templates/", http.StripPrefix("/templates", http.FileServer(http.Dir("./templates"))))
	http.Handle("/favicon.ico", http.NotFoundHandler())
	http.ListenAndServe(":8080", nil)

}

func signup(res http.ResponseWriter, req *http.Request) {
	if alreadyLoggedin(req) {
		http.Redirect(res, req, "/", http.StatusSeeOther)
		return
	}

	var u user
	if req.Method == http.MethodPost {

		//get form values
		un := req.FormValue("username")
		pw := req.FormValue("password")
		u = user{un, pw}

		// save db
		// ctx, _ := context.WithTimeout(context.Background(), 30*time.Second)
		ctx := req.Context()
		database := ConnectDB(ctx).Database("shoppeeDB")
		shopcollection := database.Collection("training")

		//username taken?
		filter := bson.D{{"username", u.Username}}
		result := shopcollection.FindOne(ctx, filter)

		var checkUser user
		err1 := result.Decode(&checkUser)
		if err1 != nil {
			insertResult, err := shopcollection.InsertOne(ctx, u)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println("Users:", insertResult.InsertedID)
		} else {
			http.Error(res, "username already taken", http.StatusForbidden)
			return
		}

		//create session
		sID := uuid.NewV4()
		c := &http.Cookie{
			Name:  "session",
			Value: sID.String(),
		}
		http.SetCookie(res, c)
		dbSessions[c.Value] = u.Username

		dbUsers[un] = u

		//redirect
		http.Redirect(res, req, "/", http.StatusSeeOther)
		return
	}
	tpl.ExecuteTemplate(res, "signup.html", u)
}

func login(res http.ResponseWriter, req *http.Request) {
	if alreadyLoggedin(req) {
		http.Redirect(res, req, "/", http.StatusSeeOther)
		return
	}
	var u user
	if req.Method == http.MethodPost {

		//get form values
		un := req.FormValue("username")
		pw := req.FormValue("password")
		u = user{un, pw}

		//connect to DB
		ctx := req.Context()
		database := ConnectDB(ctx).Database("shoppeeDB")
		shopcollection := database.Collection("training")

		//username not matched
		filter1 := bson.D{{"username", u.Username}}
		result1 := shopcollection.FindOne(ctx, filter1)

		var checkun user
		err1 := result1.Decode(&checkun)
		if err1 != nil {
			http.Error(res, "username and/or password do not match", http.StatusForbidden)
			return
		}

		//username not matched
		filter2 := bson.D{{"password", u.Password}}
		result2 := shopcollection.FindOne(ctx, filter2)

		var checkpw user
		err2 := result2.Decode(&checkpw)
		if err2 != nil {
			http.Error(res, "username and/or password do not match", http.StatusForbidden)
			return
		}

		//create session
		sID := uuid.NewV4()
		c := &http.Cookie{
			Name:  "session",
			Value: sID.String(),
		}
		http.SetCookie(res, c)
		dbSessions[c.Value] = u.Username

		dbUsers[un] = u

		//redirect
		http.Redirect(res, req, "/", http.StatusSeeOther)
		return
	}
	tpl.ExecuteTemplate(res, "login.html", u)
}

func logout(res http.ResponseWriter, req *http.Request) {
	if !alreadyLoggedin(req) {
		http.Redirect(res, req, "/", http.StatusSeeOther)
		return
	}
	c, _ := req.Cookie("session")
	//delete session
	delete(dbSessions, c.Value)
	//remove cookie
	c = &http.Cookie{
		Name:   "session",
		Value:  "",
		MaxAge: -1,
	}
	http.SetCookie(res, c)

	http.Redirect(res, req, "/", http.StatusSeeOther)
}

func index(res http.ResponseWriter, req *http.Request) {
	u := getUser(res, req)
	records := FindRecords(res, req)
	templateinput := struct {
		User    user
		Records []item
	}{
		User:    u,
		Records: records,
	}
	tpl.ExecuteTemplate(res, "index.html", templateinput)
}
func createListing(res http.ResponseWriter, req *http.Request) {
	if !alreadyLoggedin(req) {
		http.Redirect(res, req, "/", http.StatusSeeOther)
		return
	}
	var info item
	if req.Method == http.MethodPost {

		//process image
		mf, fh, err := req.FormFile("screenshot")
		if err != nil {
			fmt.Println(err)
		}
		defer mf.Close()

		//create sha for file name
		ext := strings.Split(fh.Filename, ".")[1]
		h := sha1.New()
		io.Copy(h, mf)
		fname := fmt.Sprintf("%x", h.Sum(nil)) + "." + ext

		//create new file
		wd, err := os.Getwd()
		if err != nil {
			fmt.Println(err)
		}
		path := filepath.Join(wd, "public", "pics", fname)
		nf, err := os.Create(path)
		if err != nil {
			fmt.Println(err)
		}
		defer nf.Close()

		//copy
		mf.Seek(0, 0)
		io.Copy(nf, mf)

		// save db
		ctx := req.Context()
		database := ConnectDB(ctx).Database("shoppeeDB")
		shopcollection := database.Collection("items")

		//get form values
		u := getUser(res, req)
		screenshot := fname
		name := req.FormValue("name")
		price := req.FormValue("price")
		category := req.FormValue("category")
		seller := u.Username
		info = item{screenshot, name, price, category, seller}

		insertinfo, err := shopcollection.InsertOne(ctx, info)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Items:", insertinfo.InsertedID)

		//redirect
		http.Redirect(res, req, "/", http.StatusSeeOther)
		return
	}
	tpl.ExecuteTemplate(res, "createListing.html", info)
}

func viewListing(res http.ResponseWriter, req *http.Request) {
	u := getUser(res, req)
	records := FindRecords(res, req)
	templateinput := struct {
		User    user
		Records []item
	}{
		User:    u,
		Records: records,
	}
	tpl.ExecuteTemplate(res, "viewListing.html", templateinput)
}

func getUser(res http.ResponseWriter, req *http.Request) user {
	//get cookie
	c, err := req.Cookie("session")
	if err != nil {
		sID := uuid.NewV4()
		c = &http.Cookie{
			Name:  "session",
			Value: sID.String(),
		}
	}
	http.SetCookie(res, c)

	//if user exist already, get user
	var u user
	if un, ok := dbSessions[c.Value]; ok {
		u = dbUsers[un]
	}
	return u
}

func alreadyLoggedin(req *http.Request) bool {
	c, err := req.Cookie("session")
	if err != nil {
		return false
	}
	un := dbSessions[c.Value]
	_, ok := dbUsers[un]
	return ok
}

func ConnectDB(ctx context.Context) *mongo.Client {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb+srv://shoppeeDB:training@atlascluster.sqpyiwf.mongodb.net/?retryWrites=true&w=majority"))
	if err != nil {
		log.Fatal(err)
	}
	err = client.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}
	return client
}

func FindRecords(res http.ResponseWriter, req *http.Request) []item {

	// save db
	ctx := req.Context()
	database := ConnectDB(ctx).Database("shoppeeDB")
	shopcollection := database.Collection("items")

	//find records
	//pass these options to the Find method
	findOptions := options.Find()
	//Set the limit of the number of record to find
	findOptions.SetLimit(3)
	//Define an array in which you can store the decoded documents
	var results []item

	//Passing the bson.D{{}} as the filter matches  documents in the collection
	cur, err := shopcollection.Find(ctx, bson.D{{}}, findOptions)
	if err != nil {
		log.Fatal(err)
	}
	//Finding multiple documents returns a cursor
	//Iterate through the cursor allows us to decode documents one at a time

	for cur.Next(ctx) {
		//Create a value into which the single document can be decoded
		var elem item
		err := cur.Decode(&elem)
		if err != nil {
			log.Fatal(err)
		}
		results = append(results, elem)
	}

	if err := cur.Err(); err != nil {
		log.Fatal(err)
	}

	//Close the cursor once finished
	cur.Close(context.TODO())
	// fmt.Printf("Found multiple documents: %+v\n", results)
	// fmt.Println()
	return results
}
