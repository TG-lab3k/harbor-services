package infrastructure

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/tenant/domain"
)

const appsCollection = "apps"

// FirestoreAppRepository persists apps in Firestore collection "apps".
type FirestoreAppRepository struct {
	client *firestore.Client
}

func NewFirestoreAppRepository(client *firestore.Client) *FirestoreAppRepository {
	return &FirestoreAppRepository{client: client}
}

type appDoc struct {
	AppID         string         `firestore:"app_id"`
	AppSecretHash string         `firestore:"app_secret_hash"`
	AppName       string         `firestore:"app_name"`
	RedirectURIs  []string       `firestore:"redirect_uris"`
	Status        string         `firestore:"status"`
	Settings      map[string]any `firestore:"settings"`
	CreatedAt     time.Time      `firestore:"created_at"`
	UpdatedAt     time.Time      `firestore:"updated_at"`
}

func appToDoc(app *domain.App) appDoc {
	settings := app.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	uris := app.RedirectURIs
	if uris == nil {
		uris = []string{}
	}
	return appDoc{
		AppID:         app.AppID,
		AppSecretHash: app.AppSecretHash,
		AppName:       app.AppName,
		RedirectURIs:  uris,
		Status:        string(app.Status),
		Settings:      settings,
		CreatedAt:     app.CreatedAt,
		UpdatedAt:     app.UpdatedAt,
	}
}

func docToApp(d appDoc) *domain.App {
	return &domain.App{
		AppID:         d.AppID,
		AppSecretHash: d.AppSecretHash,
		AppName:       d.AppName,
		RedirectURIs:  append([]string(nil), d.RedirectURIs...),
		Status:        domain.AppStatus(d.Status),
		Settings:      d.Settings,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
	}
}

func (r *FirestoreAppRepository) col() *firestore.CollectionRef {
	return r.client.Collection(appsCollection)
}

func (r *FirestoreAppRepository) Create(ctx context.Context, app *domain.App) error {
	_, err := r.col().Doc(app.AppID).Create(ctx, appToDoc(app))
	if status.Code(err) == codes.AlreadyExists {
		return apperr.Validation("app already exists")
	}
	return err
}

func (r *FirestoreAppRepository) GetByID(ctx context.Context, appID string) (*domain.App, error) {
	snap, err := r.col().Doc(appID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var d appDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return docToApp(d), nil
}

func (r *FirestoreAppRepository) List(ctx context.Context, filter domain.ListAppsFilter) ([]*domain.App, error) {
	var q firestore.Query
	if !filter.IncludeDisabled {
		q = r.col().Where("status", "==", string(domain.AppStatusActive))
	} else {
		q = r.col().Query
	}
	iter := q.Documents(ctx)
	defer iter.Stop()

	out := make([]*domain.App, 0)
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var d appDoc
		if err := snap.DataTo(&d); err != nil {
			return nil, err
		}
		out = append(out, docToApp(d))
	}
	return out, nil
}

func (r *FirestoreAppRepository) Update(ctx context.Context, app *domain.App) error {
	_, err := r.col().Doc(app.AppID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return apperr.AppNotFound("")
		}
		return err
	}
	_, err = r.col().Doc(app.AppID).Set(ctx, appToDoc(app))
	return err
}

func (r *FirestoreAppRepository) SoftDisable(ctx context.Context, appID string) error {
	ref := r.col().Doc(appID)
	_, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return apperr.AppNotFound("")
		}
		return err
	}
	_, err = ref.Update(ctx, []firestore.Update{
		{Path: "status", Value: string(domain.AppStatusDisabled)},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	return err
}
