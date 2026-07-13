module musicseer

go 1.24

replace (
	golang.org/x/crypto => github.com/golang/crypto v0.37.0
	golang.org/x/net => github.com/golang/net v0.39.0
	golang.org/x/sys => github.com/golang/sys v0.32.0
	golang.org/x/term => github.com/golang/term v0.31.0
	golang.org/x/text => github.com/golang/text v0.24.0
)

require (
	github.com/lib/pq v1.10.9
	github.com/mattn/go-sqlite3 v1.14.28
	golang.org/x/crypto v0.37.0
)
