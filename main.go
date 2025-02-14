package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"
)

type sightingPost struct {
	Description string  `json:"description"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

type pocketbaseSighting struct {
	Id          string `json:"id"`
	Description string `json:"description"`
}

type surrealSighting struct {
	PocketBaseId models.RecordID      `json:"id,omitempty"`
	Location     models.GeometryPoint `json:"location"`
}

type surrealDistanceResult struct {
	PocketBaseId models.RecordID      `json:"id,omitempty"`
	Location     models.GeometryPoint `json:"location"`
	Distance     float64              `json:"distance"`
}

type sightingWithDistance struct {
	Id          string  `json:"id"`
	Description string  `json:"description"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Distance    float64 `json:"distance"`
}

func main() {
	app := pocketbase.New()

	// This all came straight from the SurrealDB get started for Go : https://surrealdb.com/docs/sdk/golang/start

	// Connect to SurrealDB
	db, err := surrealdb.New("ws://localhost:8000")
	if err != nil {
		panic(err)
	}

	// Set the namespace and database
	if err = db.Use("sightings", "sightings"); err != nil {
		panic(err)
	}

	// Sign in to authentication `db`
	authData := &surrealdb.Auth{
		Username: "root", // use your setup username
		Password: "root", // use your setup password
	}
	token, err := db.SignIn(authData)
	if err != nil {
		panic(err)
	}

	// Check token validity. This is not necessary if you called `SignIn` before. This authenticates the `db` instance too if sign in was
	// not previously called
	if err := db.Authenticate(token); err != nil {
		panic(err)
	}

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {

		// /search?latitude=38.5&longitude=-106.0
		se.Router.GET("/search", func(e *core.RequestEvent) error {
			// TODO - for this example we have no auth, but you would definitely want to check the user's token and permissions here!

			latitude := e.Request.URL.Query().Get("latitude")
			longitude := e.Request.URL.Query().Get("longitude")

			fmt.Println(latitude, longitude)

			// Use a surreal spatial query to find nearby sightings
			// Note, could not get var substitution to work for the geopoint
			result, err := surrealdb.Query[[]surrealDistanceResult](db, "SELECT *, geo::distance(("+latitude+", "+longitude+"), location) as distance FROM type::table(sightings) ORDER BY distance ASC LIMIT 5", map[string]interface{}{}) // map[string]interface{}{"latlng": latlng})
			if err == nil {

				if len(*result) > 0 {

					// To return raw surreal db results
					// return e.JSON(http.StatusOK, (*result)[0].Result)

					found := make([]sightingWithDistance, 0)

					// Take each result that we got from SurrealDB and mix it with the matching record from PocketBase
					for _, res := range (*result)[0].Result {

						sighting := pocketbaseSighting{}

						err := app.DB().
							NewQuery("SELECT id, description FROM sightings WHERE id='" + res.PocketBaseId.ID.(string) + "'").
							One(&sighting)

						if err == nil {
							found = append(found, sightingWithDistance{
								Id:          sighting.Id,
								Description: sighting.Description,
								Latitude:    res.Location.Latitude,
								Longitude:   res.Location.Longitude,
								Distance:    res.Distance,
							})
						}
					}

					return e.JSON(http.StatusOK, found)
				}
			} else {
				fmt.Println("~~ Error when querying for surreal record", err)
			}

			return e.JSON(http.StatusOK, make([]interface{}, 0))
		})

		// serves static files from the provided public dir (if exists)
		se.Router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		return se.Next()
	})

	app.OnRecordCreateRequest("sightings").BindFunc(func(e *core.RecordRequestEvent) error {
		// There may be a way to grab custom data (latitude, and longitude) from record but I couldn't find it
		// Maybe a withCustomData on the client side?

		// Grab and parse the raw request payload
		body, _ := io.ReadAll(e.Request.Body)
		input := sightingPost{}
		json.Unmarshal(body, &input)

		// Grab the latitude and longitude fields that were sent with the request
		latitude := input.Latitude
		longitude := input.Longitude

		// Pack them into CustomData
		e.Record.Set("latitude", latitude)
		e.Record.Set("longitude", longitude)
		e.Record.WithCustomData(true)

		return e.Next()
	})

	// OnRecordCreateExecute has assigned a record id, but record isn't inserted yet so we can sync to external db here:
	app.OnRecordCreateExecute("sightings").BindFunc(func(e *core.RecordEvent) error {
		customData := e.Record.CustomData()

		latitude := customData["latitude"].(float64)
		longitude := customData["longitude"].(float64)

		// Send location to SurrealDB
		// TODO make PocketBaseId a unique key?
		_, err := surrealdb.Create[surrealSighting](db, models.Table("sightings"), surrealSighting{
			PocketBaseId: models.RecordID{Table: "sightings", ID: e.Record.Id},
			Location:     models.NewGeometryPoint(float64(latitude), float64(longitude)),
		})
		if err != nil {
			panic(err)
		}

		return e.Next()
	})

	app.OnRecordEnrich("sightings").BindFunc(func(e *core.RecordEnrichEvent) error {
		// A drawback of using this hook is that it makes a round trip to surrealDB for every record.
		// That will cause a slowdown when returning queries with lots of records.

		latitude := 0.0
		longitude := 0.0

		// Get matching record from SurrealDB
		surrealRecord, err := surrealdb.Select[surrealSighting, models.RecordID](db, models.RecordID{Table: "sightings", ID: e.Record.Id})
		if err == nil {
			latitude = surrealRecord.Location.Latitude
			longitude = surrealRecord.Location.Longitude
		}

		// Inject latitude and longitude back into record before it goes out the door
		e.Record.Set("latitude", latitude)
		e.Record.Set("longitude", longitude)

		// This is critical to allowing the custom fields:
		e.Record.WithCustomData(true)

		return e.Next()
	})

	app.OnRecordAfterDeleteSuccess("sightings").BindFunc(func(e *core.RecordEvent) error {
		// Cannot find right type to put into [] here...  SurrealDB docs are out of date.
		_, err = surrealdb.Delete[any](db, models.RecordID{Table: "sightings", ID: e.Record.Id})
		if err != nil {
			panic(err)
		} else {
			fmt.Println("~~ Error when removing surreal record", err)
		}

		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
