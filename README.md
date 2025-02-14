# surreal_pocket

A proof of concept app exploring the use of SurrealDB and PocketBase together.

## Setup Instructions

1) Start surreal db:

```
mkdir surreal_data
docker run --rm --pull always -p 8000:8000 -v ./surreal_data:/mydata surrealdb/surrealdb:latest-dev start --log info --user root --pass root rocksdb:/mydata/mydatabase.db
```

2) Then use Surrealist app to connect (user and password are in docker command above) and:

- Create a workspace called "sightings"
- Create a database called "sightings"

3) Start PocketBase

```
go run . serve
```

4) Import collections config

Use the PocketBase admin tool to import the schema in pb_schema.json

5) Make sure all fields in the API Rules tab for the sightings collection are left empty to give everyone access.

6) Open UI

http://localhost:8090/
