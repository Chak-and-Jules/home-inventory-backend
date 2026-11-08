package guides

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ctx = context.Background()

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	config := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var db *gorm.DB
	var err error
	if dsn := os.Getenv("GUIDE_TEST_POSTGRES_DSN"); dsn != "" {
		admin, e := gorm.Open(postgres.Open(dsn), config)
		require.NoError(t, e)
		adminConn, e := admin.DB()
		require.NoError(t, e)
		schema := "guide_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		t.Cleanup(func() {
			require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
			require.NoError(t, adminConn.Close())
		})
		db, err = gorm.Open(postgres.Open(dsn+" search_path="+schema), config)
	} else {
		db, err = gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_foreign_keys=on"), config)
	}
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	// Minimal existing tables avoid unrelated PostgreSQL-specific defaults in SQLite.
	require.NoError(t, db.Exec("CREATE TABLE profiles (id uuid PRIMARY KEY, email varchar(255))").Error)
	require.NoError(t, db.Exec("CREATE TABLE homes (id uuid PRIMARY KEY, name varchar(255))").Error)
	require.NoError(t, db.Exec("CREATE TABLE user_homes (user_id uuid, home_id uuid, role varchar(50), PRIMARY KEY(user_id, home_id))").Error)
	require.NoError(t, db.AutoMigrate(&models.GuideProgress{}, &models.GuideRequest{}))
	return db
}

type fixture struct {
	s          *Service
	user, home uuid.UUID
	now        time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testDB(t)
	f := &fixture{user: uuid.New(), home: uuid.New(), now: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}
	require.NoError(t, db.Exec("INSERT INTO profiles (id,email,created_at) VALUES (?,?,?)", f.user, "existing@example.com", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)).Error)
	require.NoError(t, db.Exec("INSERT INTO homes (id,name) VALUES (?,?)", f.home, "Personal home").Error)
	require.NoError(t, db.Exec("INSERT INTO user_homes (user_id,home_id,role) VALUES (?,?,?)", f.user, f.home, "owner").Error)
	f.s = New(db)
	f.s.Now = func() time.Time { return f.now }
	return f
}

func (f *fixture) read(t *testing.T, p Platform) View {
	t.Helper()
	list, err := f.s.List(ctx, f.user, p, 1, &f.home)
	require.NoError(t, err)
	require.NotEmpty(t, list)
	return list[0]
}

func command(p Platform, version int64, action string) Command {
	return Command{RequestID: uuid.New(), Platform: p, ClientVersion: 1, Revision: 1, ExpectedVersion: &version, Action: action}
}

func (f *fixture) act(t *testing.T, p Platform, action string, customize func(*Command)) View {
	t.Helper()
	c := command(p, f.read(t, p).Version, action)
	if customize != nil {
		customize(&c)
	}
	v, err := f.s.Act(ctx, f.user, "introduction", c)
	require.NoError(t, err)
	return v
}

func (f *fixture) complete(t *testing.T, p Platform) {
	t.Helper()
	f.act(t, p, "start", func(c *Command) { c.HomeID = &f.home })
	f.act(t, p, "checkpoint", func(c *Command) { c.StepID = "quantity" })
	f.act(t, p, "complete", nil)
}

func TestGuideLifecycleAcrossApplications(t *testing.T) {
	for _, first := range []Platform{Web, Mobile} {
		t.Run(string(first), func(t *testing.T) {
			f := newFixture(t)
			second := Mobile
			if first == Mobile {
				second = Web
			}
			initial := f.read(t, first)
			require.Equal(t, NotStarted, initial.State.Status)
			require.True(t, initial.ShouldOffer)
			require.Zero(t, initial.Version)
			// The counterpart has never been used when the first platform completes.
			f.complete(t, first)
			shared := f.read(t, second)
			require.Equal(t, Completed, shared.State.Status)
			require.False(t, shared.ShouldOffer)
			f.act(t, second, "restore", nil)
			require.Equal(t, Completed, f.read(t, first).State.Status)
			reopened := f.read(t, second)
			require.Equal(t, NotStarted, reopened.State.Status)
			require.True(t, reopened.ShouldOffer)
			// A new service instance simulates login/restart, not client-only state.
			restarted := New(f.s.DB)
			restarted.Now = f.s.Now
			list, err := restarted.List(ctx, f.user, second, 1, &f.home)
			require.NoError(t, err)
			require.True(t, list[0].ShouldOffer)
			f.act(t, second, "skip", nil)
			for _, p := range []Platform{Web, Mobile} {
				v := f.read(t, p)
				require.Equal(t, Skipped, v.State.Status)
				require.False(t, v.ShouldOffer)
			}
			f.act(t, first, "restore", nil)
			require.Equal(t, Skipped, f.read(t, second).State.Status)
			f.complete(t, first)
			require.Equal(t, Completed, f.read(t, second).State.Status)
		})
	}
}

func TestGuideDeferralCheckpointsAndReplay(t *testing.T) {
	f := newFixture(t)
	f.act(t, Web, "start", func(c *Command) { c.HomeID = &f.home })
	f.act(t, Web, "checkpoint", func(c *Command) { c.StepID = "stock" })
	deadline := f.now.Add(time.Hour)
	c := command(Web, f.read(t, Web).Version, "maybe_later")
	c.DeferUntil = &deadline
	v, err := f.s.Act(ctx, f.user, "introduction", c)
	require.NoError(t, err)
	require.Equal(t, InProgress, v.State.Status)
	require.Equal(t, "stock", v.State.StepID)
	require.False(t, v.ShouldOffer)
	mobile := f.read(t, Mobile)
	require.Equal(t, NotStarted, mobile.State.Status)
	require.Nil(t, mobile.State.DeferredUntil)
	require.True(t, mobile.ShouldOffer)
	f.now = deadline.Add(time.Second)
	require.True(t, f.read(t, Web).ShouldOffer)
	f.act(t, Web, "restore", nil)
	// Replaying a successful old deferral must not undo the later restore.
	afterRestore := f.read(t, Web)
	v, err = f.s.Act(ctx, f.user, "introduction", c)
	require.NoError(t, err)
	require.Equal(t, afterRestore.Version, v.Version)
	require.Equal(t, NotStarted, v.State.Status)
	require.Nil(t, v.State.DeferredUntil)
	c.DeferUntil = nil
	c.Action = "skip"
	_, err = f.s.Act(ctx, f.user, "introduction", c)
	require.ErrorIs(t, err, ErrRequestReuse)
	// The platform checkpoint is validated against that platform's step list.
	f.act(t, Mobile, "start", func(c *Command) { c.HomeID = &f.home })
	wrong := command(Mobile, f.read(t, Mobile).Version, "checkpoint")
	wrong.StepID = "stock"
	_, err = f.s.Act(ctx, f.user, "introduction", wrong)
	require.ErrorIs(t, err, ErrTransition)
	f.act(t, Mobile, "checkpoint", func(c *Command) { c.StepID = "inventory" })
	require.Empty(t, f.read(t, Web).State.StepID)
}

func TestGuideStaleSavesAndInvalidActions(t *testing.T) {
	f := newFixture(t)
	f.act(t, Web, "start", func(c *Command) { c.HomeID = &f.home })
	stale := command(Web, f.read(t, Web).Version, "checkpoint")
	stale.StepID = "stock"
	f.act(t, Mobile, "skip", nil)
	_, err := f.s.Act(ctx, f.user, "introduction", stale)
	require.ErrorIs(t, err, ErrConflict)
	f.act(t, Web, "restore", nil)
	_, err = f.s.Act(ctx, f.user, "introduction", stale)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, NotStarted, f.read(t, Web).State.Status)
	for _, action := range []string{"checkpoint", "complete"} {
		c := command(Web, f.read(t, Web).Version, action)
		c.HomeID = &f.home
		if action == "checkpoint" {
			c.StepID = "quantity"
		}
		_, err = f.s.Act(ctx, f.user, "introduction", c)
		require.ErrorIs(t, err, ErrTransition)
	}
	f.act(t, Web, "start", func(c *Command) { c.HomeID = &f.home })
	c := command(Web, f.read(t, Web).Version, "complete")
	_, err = f.s.Act(ctx, f.user, "introduction", c)
	require.ErrorIs(t, err, ErrTransition)
	c = command(Web, f.read(t, Web).Version, "maybe_later")
	past := f.now.Add(-time.Second)
	c.DeferUntil = &past
	_, err = f.s.Act(ctx, f.user, "introduction", c)
	require.ErrorIs(t, err, ErrTransition)
	require.Equal(t, InProgress, f.read(t, Web).State.Status)
}

func TestGuidePublicationCompatibilityPrerequisitesAndRevisions(t *testing.T) {
	f := newFixture(t)
	intro := f.s.Definitions[0]
	feature := intro
	feature.ID = "feature"
	feature.Prerequisites = []string{"introduction"}
	future := intro
	future.ID = "future"
	future.PublishedAt = f.now.Add(time.Hour)
	inactive := intro
	inactive.ID = "inactive"
	inactive.Active = false
	webOnly := intro
	webOnly.ID = "web-only"
	webOnly.Variants = map[Platform]Variant{Web: intro.Variants[Web]}
	upgrade := intro
	upgrade.ID = "upgrade"
	variant := intro.Variants[Web]
	variant.MinClientVersion = 2
	upgrade.Variants = map[Platform]Variant{Web: variant}
	f.s.Definitions = []Definition{intro, feature, future, inactive, webOnly, upgrade}
	list, err := f.s.List(ctx, f.user, Web, 1, &f.home)
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Contains(t, list[1].UnavailableReasons, "prerequisite_required")
	for _, id := range []string{"future", "inactive", "upgrade"} {
		_, err = f.s.Act(ctx, f.user, id, command(Web, 0, "skip"))
		require.ErrorIs(t, err, ErrNotFound)
	}
	list, err = f.s.List(ctx, f.user, Mobile, 1, &f.home)
	require.NoError(t, err)
	require.Len(t, list, 2)
	f.complete(t, Web)
	list, err = f.s.List(ctx, f.user, Mobile, 1, &f.home)
	require.NoError(t, err)
	require.True(t, list[1].ShouldOffer)
	f.s.Definitions[0].Variants[Web] = Variant{ContentVersion: 2, MinClientVersion: 1, EntryPoint: "session", Steps: intro.Variants[Web].Steps}
	require.Equal(t, Completed, f.read(t, Web).State.Status)
	f.s.Definitions[0].Revision = 2 // deliberate workflow reintroduction
	require.Equal(t, NotStarted, f.read(t, Web).State.Status)
	c := command(Web, 0, "skip")
	_, err = f.s.Act(ctx, f.user, "introduction", c)
	require.ErrorIs(t, err, ErrConflict)
	c.Revision = 2
	_, err = f.s.Act(ctx, f.user, "introduction", c)
	require.NoError(t, err)
	// The other logical guide has not been marked completed/skipped by propagation.
	p, err := progress(f.s.DB, f.user, feature)
	require.NoError(t, err)
	require.Equal(t, NotStarted, p.WebStatus)
	f.now = f.now.Add(2 * time.Hour)
	list, err = f.s.List(ctx, f.user, Web, 1, &f.home)
	require.NoError(t, err)
	var published *View
	for i := range list {
		if list[i].ID == "future" {
			published = &list[i]
		}
	}
	require.NotNil(t, published)
	require.True(t, published.ShouldOffer)
	require.Equal(t, NotStarted, published.State.Status)
}

func TestGuideHomePermissionsUserIsolationAndDeletion(t *testing.T) {
	f := newFixture(t)
	other := uuid.New()
	foreign := uuid.New()
	require.NoError(t, f.s.DB.Exec("INSERT INTO profiles (id,email) VALUES (?,?)", other, "other@example.com").Error)
	require.NoError(t, f.s.DB.Exec("INSERT INTO homes (id,name) VALUES (?,?)", foreign, "Other home").Error)
	c := command(Web, 0, "start")
	c.HomeID = &foreign
	_, err := f.s.Act(ctx, f.user, "introduction", c)
	require.ErrorIs(t, err, ErrHome)
	require.NoError(t, f.s.DB.Exec("UPDATE user_homes SET role='viewer' WHERE user_id=?", f.user).Error)
	c.HomeID = &f.home
	_, err = f.s.Act(ctx, f.user, "introduction", c)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, f.s.DB.Exec("UPDATE user_homes SET role='editor' WHERE user_id=?", f.user).Error)
	f.act(t, Web, "start", func(c *Command) { c.HomeID = &f.home })
	require.NoError(t, f.s.DB.Exec("DELETE FROM user_homes WHERE user_id=?", f.user).Error)
	views, err := f.s.List(ctx, f.user, Web, 1, nil)
	require.NoError(t, err)
	require.Nil(t, views[0].State.HomeID)
	require.False(t, views[0].Available)
	c = command(Web, views[0].Version, "checkpoint")
	c.StepID = "stock"
	_, err = f.s.Act(ctx, f.user, "introduction", c)
	require.ErrorIs(t, err, ErrHome)
	// A different user cannot inherit progress or replay receipts from this user.
	list, err := f.s.List(ctx, other, Web, 1, nil)
	require.NoError(t, err)
	require.Zero(t, list[0].Version)
	require.Equal(t, NotStarted, list[0].State.Status)
	f.act(t, Web, "skip", nil)
	require.NoError(t, f.s.DB.Exec("DELETE FROM homes WHERE id=?", f.home).Error)
	var saved models.GuideProgress
	require.NoError(t, f.s.DB.First(&saved, "user_id=?", f.user).Error)
	require.Nil(t, saved.WebHomeID)
	require.NoError(t, f.s.DB.Exec("DELETE FROM profiles WHERE id=?", f.user).Error)
	var count int64
	require.NoError(t, f.s.DB.Model(&models.GuideProgress{}).Where("user_id=?", f.user).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.s.DB.Model(&models.GuideRequest{}).Where("user_id=?", f.user).Count(&count).Error)
	require.Zero(t, count)
}

func TestGuideRollbackAndValidation(t *testing.T) {
	f := newFixture(t)
	// Failing the receipt insert must roll back both platform updates.
	name := "fail_receipt"
	require.NoError(t, f.s.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "guide_requests" {
			tx.AddError(errors.New("receipt unavailable"))
		}
	}))
	_, err := f.s.Act(ctx, f.user, "introduction", command(Web, 0, "skip"))
	require.Error(t, err)
	require.NoError(t, f.s.DB.Callback().Create().Remove(name))
	require.Zero(t, f.read(t, Web).Version)
	require.Equal(t, NotStarted, f.read(t, Mobile).State.Status)
	base := command(Web, 0, "skip")
	for _, change := range []func(*Command){
		func(c *Command) { c.ExpectedVersion = nil }, func(c *Command) { c.RequestID = uuid.Nil }, func(c *Command) { c.Platform = "android" },
		func(c *Command) { c.ClientVersion = 0 }, func(c *Command) { c.Revision = 0 }, func(c *Command) { c.Action = "unknown" },
		func(c *Command) { c.StepID = "stock" }, func(c *Command) { c.HomeID = &f.home },
	} {
		c := base
		change(&c)
		_, err = f.s.Act(ctx, f.user, "introduction", c)
		require.ErrorIs(t, err, ErrInvalid)
	}
	_, err = f.s.List(ctx, f.user, "unknown", 1, nil)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestGuideChangingHomeStartsNewContext(t *testing.T) {
	f := newFixture(t)
	f.act(t, Web, "start", func(c *Command) { c.HomeID = &f.home })
	f.act(t, Web, "checkpoint", func(c *Command) { c.StepID = "quantity" })
	second := uuid.New()
	require.NoError(t, f.s.DB.Exec("INSERT INTO homes (id,name) VALUES (?,?)", second, "Second home").Error)
	require.NoError(t, f.s.DB.Exec("INSERT INTO user_homes (user_id,home_id,role) VALUES (?,?,?)", f.user, second, "owner").Error)
	c := command(Web, f.read(t, Web).Version, "complete")
	c.HomeID = &second
	_, err := f.s.Act(ctx, f.user, "introduction", c)
	require.ErrorIs(t, err, ErrTransition)
	v := f.act(t, Web, "start", func(c *Command) { c.HomeID = &second })
	require.Equal(t, "home", v.State.StepID)
	require.Equal(t, &second, v.State.HomeID)
}

func TestGuideCatalogValidation(t *testing.T) {
	require.NoError(t, ValidateCatalog(Catalog()))
	for _, mutate := range []func([]Definition) []Definition{
		func(c []Definition) []Definition { return append(c, c[0]) },
		func(c []Definition) []Definition { c[0].Prerequisites = []string{"missing"}; return c },
		func(c []Definition) []Definition { c[0].Prerequisites = []string{"introduction"}; return c },
		func(c []Definition) []Definition { c[0].Variants[Web] = Variant{}; return c },
	} {
		require.Error(t, ValidateCatalog(mutate(Catalog())))
	}
}

func TestGuideRepeatedMigrationPreservesRestoreAndReceipts(t *testing.T) {
	f := newFixture(t)
	c := command(Web, 0, "skip")
	_, err := f.s.Act(ctx, f.user, "introduction", c)
	require.NoError(t, err)
	f.act(t, Mobile, "restore", nil)
	require.NoError(t, f.s.DB.AutoMigrate(&models.GuideProgress{}, &models.GuideRequest{}))
	require.Equal(t, NotStarted, f.read(t, Mobile).State.Status)
	require.Equal(t, Skipped, f.read(t, Web).State.Status)
	_, err = f.s.Act(ctx, f.user, "introduction", c)
	require.NoError(t, err)
	require.Equal(t, NotStarted, f.read(t, Mobile).State.Status)
	var profile models.Profile
	require.NoError(t, f.s.DB.First(&profile, "id=?", f.user).Error)
	require.Equal(t, "existing@example.com", profile.Email)
}

func TestGuideConcurrentCommandsPostgres(t *testing.T) {
	if os.Getenv("GUIDE_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL for real concurrent transactions")
	}
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate=%v", duplicate), func(t *testing.T) {
			f := newFixture(t)
			var wg sync.WaitGroup
			start := make(chan struct{})
			results := make(chan error, 8)
			shared := command(Web, 0, "skip")
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					c := shared
					if !duplicate {
						c.RequestID = uuid.New()
					}
					<-start
					_, err := f.s.Act(ctx, f.user, "introduction", c)
					results <- err
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			success, conflicts := 0, 0
			for err := range results {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, ErrConflict)
					conflicts++
				}
			}
			if duplicate {
				require.Equal(t, 8, success)
			} else {
				require.Equal(t, 1, success)
				require.Equal(t, 7, conflicts)
			}
			require.EqualValues(t, 1, f.read(t, Web).Version)
			require.Equal(t, Skipped, f.read(t, Mobile).State.Status)
		})
	}
}

func TestGuideConcurrentRequestReuseAcrossGuidesPostgres(t *testing.T) {
	if os.Getenv("GUIDE_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL for concurrent receipt conflicts")
	}
	f := newFixture(t)
	other := f.s.Definitions[0]
	other.ID = "other"
	f.s.Definitions = append(f.s.Definitions, other)
	c := command(Web, 0, "skip")
	start := make(chan struct{})
	errorsFound := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"introduction", "other"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			_, err := f.s.Act(ctx, f.user, id, c)
			errorsFound <- err
		}(id)
	}
	close(start)
	wg.Wait()
	close(errorsFound)
	success, reused := 0, 0
	for err := range errorsFound {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrRequestReuse)
			reused++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, reused)
	list, err := f.s.List(ctx, f.user, Web, 1, &f.home)
	require.NoError(t, err)
	require.EqualValues(t, 1, list[0].Version+list[1].Version)
}
