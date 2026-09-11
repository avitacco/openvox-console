package rbac

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestCreateUser_HashesPassword(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "rbac-store-test-create", "correct horse battery staple")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	if u.PasswordHash == "correct horse battery staple" {
		t.Fatal("password stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("correct horse battery staple")); err != nil {
		t.Errorf("stored hash does not verify against the original password: %v", err)
	}
}

func TestGetUserByUsername(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "rbac-store-test-getbyusername", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), created.ID) })

	got, err := s.GetUserByUsername(ctx, "rbac-store-test-getbyusername")
	if err != nil {
		t.Fatalf("GetUserByUsername() error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
}

func TestGetUserByUsername_NotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetUserByUsername(context.Background(), "rbac-store-test-does-not-exist")
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestDeleteUser_PreventsFutureLookup(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "rbac-store-test-delete", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	if err := s.DeleteUser(ctx, created.ID); err != nil {
		t.Fatalf("DeleteUser() error: %v", err)
	}

	_, err = s.GetUser(ctx, created.ID)
	if err != ErrNotFound {
		t.Errorf("GetUser() after delete error = %v, want ErrNotFound", err)
	}
}

func TestCreateOIDCUser_AndGetByOIDCSubject(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-store-test-oidc-sub-1", "oidc-store-test-user")
	if err != nil {
		t.Fatalf("CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	if u.OIDCSubject == nil || *u.OIDCSubject != "rbac-store-test-oidc-sub-1" {
		t.Fatalf("OIDCSubject = %v, want rbac-store-test-oidc-sub-1", u.OIDCSubject)
	}

	// The stored password hash must not verify against any predictable
	// value - it exists only so password login naturally fails for this
	// account, never so it can succeed.
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("")); err == nil {
		t.Error("OIDC-provisioned user's password hash verifies against an empty password")
	}

	got, err := s.GetUserByOIDCSubject(ctx, "rbac-store-test-oidc-sub-1")
	if err != nil {
		t.Fatalf("GetUserByOIDCSubject() error: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("ID = %d, want %d", got.ID, u.ID)
	}
}

func TestGetUserByOIDCSubject_NotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetUserByOIDCSubject(context.Background(), "rbac-store-test-oidc-sub-does-not-exist")
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestCreateOIDCUser_DuplicateSubjectRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-store-test-oidc-sub-dup", "oidc-store-test-dup-1")
	if err != nil {
		t.Fatalf("first CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	_, err = s.CreateOIDCUser(ctx, "rbac-store-test-oidc-sub-dup", "oidc-store-test-dup-2")
	if err != ErrDuplicateName {
		t.Errorf("second CreateOIDCUser() with the same subject error = %v, want ErrDuplicateName", err)
	}
}

func TestUpdatePassword(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "rbac-store-test-updatepw", "old-password")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), created.ID) })

	if err := s.UpdatePassword(ctx, created.ID, "new-password"); err != nil {
		t.Fatalf("UpdatePassword() error: %v", err)
	}

	got, err := s.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("new-password")); err != nil {
		t.Error("password was not actually updated")
	}
}

func TestUpdateProfile(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "rbac-store-test-updateprofile", "password")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), created.ID) })

	if created.FirstName != nil || created.LastName != nil || created.Email != nil {
		t.Fatalf("newly created user = %+v, want no profile set", created)
	}

	if err := s.UpdateProfile(ctx, created.ID, "Ada", "Lovelace", "ada@example.com"); err != nil {
		t.Fatalf("UpdateProfile() error: %v", err)
	}

	got, err := s.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}
	if derefOrEmpty(got.FirstName) != "Ada" || derefOrEmpty(got.LastName) != "Lovelace" || derefOrEmpty(got.Email) != "ada@example.com" {
		t.Errorf("got = %+v, want Ada Lovelace <ada@example.com>", got)
	}

	// An empty string clears a field back to unset (nil), rather than
	// storing a literal empty string - see UpdateProfile's own doc.
	if err := s.UpdateProfile(ctx, created.ID, "Ada", "", "ada@example.com"); err != nil {
		t.Fatalf("UpdateProfile() (clear last name) error: %v", err)
	}
	got, err = s.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}
	if got.LastName != nil {
		t.Errorf("LastName = %v, want nil after clearing", got.LastName)
	}
}

func TestUpdateProfile_NotFound(t *testing.T) {
	s := testStore(t)
	if err := s.UpdateProfile(context.Background(), -1, "Ada", "Lovelace", "ada@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateProfile() error = %v, want ErrNotFound", err)
	}
}
