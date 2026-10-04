package instance_service

import (
	"errors"
	"net/http"
	"testing"

	"gorm.io/gorm"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	instance_repository "github.com/lucasgiovannibr/whatygo/pkg/instance/repository"
)

type tokenRepo struct {
	instance_repository.InstanceRepository
	tokens  map[string]bool
	created int
}

func (r *tokenRepo) GetInstanceByName(string) (*instance_model.Instance, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *tokenRepo) GetInstanceByToken(t string) (*instance_model.Instance, error) {
	if r.tokens[t] {
		return &instance_model.Instance{Token: t}, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *tokenRepo) Create(i instance_model.Instance) (*instance_model.Instance, error) {
	r.created++
	return &i, nil
}

// The token column is unique; a duplicate used to reach the database and come back as a 500.
func TestCreateRefusesATokenAnotherInstanceHas(t *testing.T) {
	repo := &tokenRepo{tokens: map[string]bool{"taken-token-0000000000": true}}
	svc := instances{instanceRepository: repo, config: &config.Config{}}

	_, err := svc.Create(&CreateStruct{Name: "n", Token: "taken-token-0000000000"})
	var apiErr *apierror.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("want a 409 conflict, got %v", err)
	}
	if repo.created != 0 {
		t.Fatal("nothing may be created")
	}
	if _, err := svc.Create(&CreateStruct{Name: "n", Token: "another-token-000000000"}); err != nil || repo.created != 1 {
		t.Fatalf("a new token creates the instance: %v (%d)", err, repo.created)
	}
}
