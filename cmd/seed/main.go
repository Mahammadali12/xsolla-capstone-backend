// Seed CLI: go run ./cmd/seed admin <email> <password>
// Creates an admin account or promotes an existing user to admin.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
	_ "github.com/lib/pq"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/seed admin <email> <password>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "admin":
		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, "usage: go run ./cmd/seed admin <email> <password>")
			os.Exit(1)
		}
		if err := seedAdmin(os.Args[2], os.Args[3]); err != nil {
			log.Fatalf("seed admin: %v", err)
		}
		fmt.Println("done")
	default:
		fmt.Fprintf(os.Stderr, "unknown command %s\n", os.Args[1])
		os.Exit(1)
	}
}

func seedAdmin(email, password string) error {
	db, err := sql.Open("postgres", "host=127.0.0.1 port=5432 user=maqa password=maqa dbname=lms sslmode=disable")
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()
	st := store.NewStore(db)

	hash, err := auth.Hash(password)
	if err != nil {
		return err
	}

	user, err := st.GetUserByEmail(ctx, email)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		id, err := st.CreateUser(ctx, email, email, []string{"admin"}, hash)
		if err != nil {
			return err
		}
		fmt.Printf("created admin %s (id=%d)\n", email, id)
		return nil
	case err != nil:
		return err
	}

	hasAdmin := false
	for _, r := range user.Roles {
		if r == "admin" {
			hasAdmin = true
			break
		}
	}
	if hasAdmin {
		fmt.Printf("%s is already admin\n", email)
		return nil
	}

	roles := append(user.Roles, "admin")
	if err := st.ReplaceUserRoles(ctx, user.ID, roles, nil); err != nil {
		return err
	}
	fmt.Printf("promoted %s to admin\n", email)
	return nil
}
