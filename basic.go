// Package auth implements Basic authentication.
package auth

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/codegangsta/negroni"
	"github.com/pmylund/go-cache"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultCacheExpireTime = 10 * time.Minute
	defaultCachePurseTime  = 60 * time.Second
	bcryptCost             = 12
)

// DataStore is an interface for retrieving hashed password by userid.
type DataStore interface {
	Get(userId string) (hashedPassword []byte, found bool)
}

// SimpleBasic is a simple DataStore that store only one userid, hashed password pair.
type SimpleBasic struct {
	UserId         string
	HashedPassword []byte
}

// SimpleBasic.Get returns hashed password by userid.
func (d *SimpleBasic) Get(userId string) ([]byte, bool) {
	if userId == d.UserId {
		return d.HashedPassword, true
	}
	return nil, false
}

// NewSimpleBasic returns SimpleBasic builded from userid, password
func NewSimpleBasic(userId, password string) (DataStore, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, err
	}

	return &SimpleBasic{
		UserId:         userId,
		HashedPassword: hashedPassword,
	}, nil
}

// requireAuth writes error to client which initiates the authentication process
// or requires reauthentication.
func requireAuth(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Basic realm=\"Authorization Required\"")
	http.Error(w, "Not Authorized", http.StatusUnauthorized)
}

// getCred get userid, password from request.
func getCred(req *http.Request) (string, string) {
	// Split authorization header.
	s := strings.SplitN(req.Header.Get("Authorization"), " ", 2)
	if len(s) != 2 || s[0] != "Basic" {
		return "", ""
	}

	// Decode credential.
	cred, err := base64.StdEncoding.DecodeString(s[1])
	if err != nil {
		return "", ""
	}

	// Split credential into userid, password.
	pair := strings.SplitN(string(cred), ":", 2)
	if len(pair) != 2 {
		return "", ""
	}

	return pair[0], pair[1]
}

// NewBasic returns a negroni.HandlerFunc that authenticates via Basic auth using data store.
// Writes a http.StatusUnauthorized if authentication fails.
func NewBasic(dataStore DataStore) negroni.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		// Extract userid, password from request.
		userId, password := getCred(req)

		if userId == "" {
			requireAuth(w)
			return
		}

		// Extract hashed passwor from credentials.
		hashedPassword, found := dataStore.Get(userId)
		if !found {
			requireAuth(w)
			return
		}

		// Check if the password is correct.
		err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
		// Password not correct. Fail.
		if err != nil {
			requireAuth(w)
			return
		}

		r := w.(negroni.ResponseWriter)

		// Password correct.
		if r.Status() != http.StatusUnauthorized {
			next(w, req)
		}
	}
}

// Basic returns a negroni.HandlerFunc that authenticates via Basic Auth.
// Writes a http.StatusUnauthorized if authentication fails.
func Basic(userid, password string) negroni.HandlerFunc {
	dataStore, err := NewSimpleBasic(userid, password)
	if err != nil {
		return nil
	}

	return NewBasic(dataStore)
}

// CacheBasic returns a negroni.HandlerFunc that authenticates via Basic auth using cache.
// Writes a http.StatusUnauthorized if authentication fails.
func CacheBasic(dataStore DataStore, cacheExpireTime, cachePurseTime time.Duration) negroni.HandlerFunc {
	var basic = NewBasic(dataStore)
	var c = cache.New(cacheExpireTime, cachePurseTime)

	return func(w http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		// Get credential from request header.
		credential := req.Header.Get("Authorization")
		// Get authentication status by credential.
		authenticated, found := c.Get(credential)

		// Cache hit
		if found && (authenticated == "true") {
			next(w, req)
		} else { // Cache miss. Unauthenticated.
			basic(w, req, next)
			r := w.(negroni.ResponseWriter)

			// Password correct.
			if r.Status() != http.StatusUnauthorized {
				c.Set(credential, "true", cache.DefaultExpiration)
			}
		}
	}
}

// CacheBasicDefault returns a negroni.HandlerFunc that authenticates via Basic auth using cache.
// with default cache configuration. Writes a http.StatusUnauthorized if authentication fails.
func CacheBasicDefault(dataStore DataStore) negroni.HandlerFunc {
	return CacheBasic(dataStore, defaultCacheExpireTime, defaultCachePurseTime)
}
