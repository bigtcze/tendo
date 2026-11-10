package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/subject"
)

// pauseBeforeUpdateRepository pauses Update between the service's stored read
// and the version-guarded write, making the read/write race deterministic.
type pauseBeforeUpdateRepository struct {
	subject.Repository
	targetID string
	readDone chan struct{}
	proceed  chan struct{}
	once     sync.Once
}

func (p *pauseBeforeUpdateRepository) Get(ctx context.Context, householdID, subjectID string) (subject.Subject, error) {
	found, err := p.Repository.Get(ctx, householdID, subjectID)
	if subjectID == p.targetID {
		p.once.Do(func() {
			close(p.readDone)
			<-p.proceed
		})
	}
	return found, err
}

func TestTransferMemberPersonNegativeCasesAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	app := connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	const (
		home        = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
		actor       = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
		member      = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
		current     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
		target      = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
		otherMember = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b72"
		foreignHome = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81"
		foreignUser = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82"
		foreignSubj = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b83"
	)
	setup := func() {
		t.Helper()
		if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO households(id,name,timezone) VALUES($1::uuid,'negative home','UTC')`, home); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(id,login) VALUES($1::uuid,'neg_actor'),($2::uuid,'neg_member'),($3::uuid,'neg_other')`, actor, member, otherMember); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$4::uuid,'admin'),($2::uuid,$4::uuid,'member'),($3::uuid,$4::uuid,'member')`, actor, member, otherMember, home); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO subjects(id,household_id,type,name,linked_user_id) VALUES
			($1::uuid,$4::uuid,'person','Member person',$2::uuid),
			($3::uuid,$4::uuid,'person','Tomáš',NULL)`, current, member, target, home); err != nil {
			t.Fatal(err)
		}
	}
	allow := func(context.Context, string, string) error { return nil }
	newService := func() *subject.Service { return subject.NewService(NewRepository(app), allow, allow) }

	for _, tc := range []struct {
		name    string
		mutate  func()
		request subject.TransferRequest
		member  string
		check   func(error) bool
	}{
		{"archived target", func() {
			if _, err := admin.Exec(ctx, `UPDATE subjects SET archived=true WHERE id=$1::uuid`, target); err != nil {
				t.Fatal(err)
			}
		}, subject.TransferRequest{SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1}, member,
			func(err error) bool { return isNotEligible(err) }},
		{"non-person target", func() {
			if _, err := admin.Exec(ctx, `UPDATE subjects SET type='vehicle' WHERE id=$1::uuid`, target); err != nil {
				t.Fatal(err)
			}
		}, subject.TransferRequest{SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1}, member,
			func(err error) bool { return isNotEligible(err) }},
		{"target already linked", func() {
			if _, err := admin.Exec(ctx, `UPDATE subjects SET linked_user_id=$1::uuid WHERE id=$2::uuid`, otherMember, target); err != nil {
				t.Fatal(err)
			}
		}, subject.TransferRequest{SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1}, member,
			func(err error) bool { return isNotEligible(err) }},
		{"same target and current", func() {}, subject.TransferRequest{SubjectID: current, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1}, member,
			func(err error) bool { return isNotEligible(err) }},
		{"cross-household subject", func() {
			if _, err := admin.Exec(ctx, `INSERT INTO households(id,name,timezone) VALUES($1::uuid,'foreign','UTC')`, foreignHome); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(id,login) VALUES($1::uuid,'foreign_user')`, foreignUser); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, foreignUser, foreignHome); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, `INSERT INTO subjects(id,household_id,type,name,linked_user_id) VALUES($1::uuid,$2::uuid,'person','Foreign',$3::uuid)`, foreignSubj, foreignHome, foreignUser); err != nil {
				t.Fatal(err)
			}
		}, subject.TransferRequest{SubjectID: foreignSubj, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1}, member,
			func(err error) bool { return errors.Is(err, subject.ErrNotFound) }},
		{"cross-household member", func() {
			if _, err := admin.Exec(ctx, `INSERT INTO households(id,name,timezone) VALUES($1::uuid,'foreign','UTC')`, foreignHome); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(id,login) VALUES($1::uuid,'foreign_user')`, foreignUser); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, foreignUser, foreignHome); err != nil {
				t.Fatal(err)
			}
		}, subject.TransferRequest{SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1}, foreignUser,
			func(err error) bool { return errors.Is(err, subject.ErrNotFound) }},
		{"stale target version", func() {}, subject.TransferRequest{SubjectID: target, SubjectVersion: 9, CurrentSubjectID: current, CurrentSubjectVersion: 1}, member,
			func(err error) bool { return errors.Is(err, subject.ErrVersionMismatch) }},
		{"stale current version", func() {}, subject.TransferRequest{SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 9}, member,
			func(err error) bool { return errors.Is(err, subject.ErrVersionMismatch) }},
		{"wrong current", func() {}, subject.TransferRequest{SubjectID: target, SubjectVersion: 1, CurrentSubjectID: target, CurrentSubjectVersion: 1}, member,
			func(err error) bool { return errors.Is(err, subject.ErrMemberPersonChanged) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup()
			tc.mutate()
			_, err := newService().Transfer(ctx, actor, home, tc.member, tc.request)
			if !tc.check(err) {
				t.Fatalf("transfer err=%v", err)
			}
			current, err := NewRepository(app).Get(ctx, home, current)
			if err != nil || current.LinkedUserID == nil || *current.LinkedUserID != member || current.Version != 1 {
				t.Fatalf("rejected transfer changed current=%+v err=%v", current, err)
			}
		})
	}
}

func isNotEligible(err error) bool {
	var validation *subject.ValidationError
	return errors.As(err, &validation) && validation.Field == "subjectId" && validation.Code == "not_eligible"
}

func TestTransferRollsBackWhenSecondWriteFails(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	app := connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	const (
		home    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
		actor   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
		member  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
		current = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
		target  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO households(id,name,timezone) VALUES($1::uuid,'rollback home','UTC')`, home); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(id,login) VALUES($1::uuid,'rollback_actor'),($2::uuid,'rollback_member')`, actor, member); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$3::uuid,'admin'),($2::uuid,$3::uuid,'member')`, actor, member, home); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO subjects(id,household_id,type,name,linked_user_id) VALUES
		($1::uuid,$4::uuid,'person','Member person',$2::uuid),
		($3::uuid,$4::uuid,'person','Tomáš',NULL)`, current, member, target, home); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE FUNCTION fail_target_link() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id = '`+target+`'::uuid THEN RAISE EXCEPTION 'injected second write failure'; END IF;
 RETURN NEW;
END $$`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(ctx, `DROP TRIGGER IF EXISTS fail_target_link ON subjects; DROP FUNCTION IF EXISTS fail_target_link()`)
	}()
	if _, err := admin.Exec(ctx, `CREATE TRIGGER fail_target_link BEFORE UPDATE ON subjects FOR EACH ROW EXECUTE FUNCTION fail_target_link()`); err != nil {
		t.Fatal(err)
	}
	allow := func(context.Context, string, string) error { return nil }
	service := subject.NewService(NewRepository(app), allow, allow)
	if _, err := service.Transfer(ctx, actor, home, member, subject.TransferRequest{
		SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1,
	}); err == nil {
		t.Fatal("transfer succeeded despite second write failure")
	}
	repo := NewRepository(app)
	currentAfter, err := repo.Get(ctx, home, current)
	if err != nil || currentAfter.LinkedUserID == nil || *currentAfter.LinkedUserID != member || currentAfter.Version != 1 {
		t.Fatalf("first write was not rolled back: current=%+v err=%v", currentAfter, err)
	}
	targetAfter, err := repo.Get(ctx, home, target)
	if err != nil || targetAfter.LinkedUserID != nil || targetAfter.Version != 1 {
		t.Fatalf("second write target changed: target=%+v err=%v", targetAfter, err)
	}
}

func TestStaleFutureVersionCannotModifyMemberPersonAfterTransfer(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	app := connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	const (
		home    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
		actor   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
		member  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
		current = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
		target  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO households(id,name,timezone) VALUES($1::uuid,'stale future home','UTC')`, home); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(id,login) VALUES($1::uuid,'stale_future_actor'),($2::uuid,'stale_future_member')`, actor, member); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$3::uuid,'admin'),($2::uuid,$3::uuid,'member')`, actor, member, home); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO subjects(id,household_id,type,name,linked_user_id) VALUES
		($1::uuid,$4::uuid,'person','Member person',$2::uuid),
		($3::uuid,$4::uuid,'person','Tomáš',NULL)`, current, member, target, home); err != nil {
		t.Fatal(err)
	}
	allow := func(context.Context, string, string) error { return nil }
	repo := NewRepository(app)
	paused := &pauseBeforeUpdateRepository{Repository: repo, targetID: target, readDone: make(chan struct{}), proceed: make(chan struct{})}
	updateService := subject.NewService(paused, allow, allow)
	transferService := subject.NewService(repo, allow, allow)

	archived := true
	updateResult := make(chan error, 1)
	go func() {
		_, err := updateService.Update(ctx, actor, home, target, 2, subject.Patch{Archived: &archived})
		updateResult <- err
	}()
	select {
	case <-paused.readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("stored read did not happen")
	}
	if _, err := transferService.Transfer(ctx, actor, home, member, subject.TransferRequest{
		SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1,
	}); err != nil {
		t.Fatalf("concurrent transfer: %v", err)
	}
	close(paused.proceed)
	if err := <-updateResult; !errors.Is(err, subject.ErrVersionMismatch) {
		t.Fatalf("future-version update err=%v, want version mismatch before any write", err)
	}
	person, err := repo.GetMemberPerson(ctx, home, member)
	if err != nil || person.ID != target || person.Archived || person.Version != 2 || person.LinkedUserID == nil || *person.LinkedUserID != member {
		t.Fatalf("canonical person after rejected update=%+v err=%v", person, err)
	}
}

func TestTransferMemberPersonAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := connect(t, ctx, adminURL)
	app := connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	const (
		home    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
		actor   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
		member  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
		current = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
		target  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
		target2 = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b72"
	)
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
			t.Fatalf("reset: %v", err)
		}
		if _, err := admin.Exec(c, `INSERT INTO households(id,name,timezone) VALUES($1::uuid,'transfer home','UTC')`, home); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(c, `INSERT INTO user_accounts(id,login) VALUES($1::uuid,'transfer_actor'),($2::uuid,'transfer_member')`, actor, member); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(c, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$3::uuid,'admin'),($2::uuid,$3::uuid,'member')`, actor, member, home); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(c, `INSERT INTO subjects(id,household_id,type,name,linked_user_id) VALUES
			($1::uuid,$4::uuid,'person','Member person',$2::uuid),
			($3::uuid,$4::uuid,'person','Tomáš',NULL)`, current, member, target, home); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	members := func(ctx context.Context, userID, householdID string) error {
		var exists bool
		if err := app.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid)`, userID, householdID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return subject.ErrNotFound
		}
		return nil
	}
	service := subject.NewService(NewRepository(app), func(context.Context, string, string) error { return nil }, members)

	linkedPerson, err := service.GetMemberPerson(ctx, actor, home, member)
	if err != nil || linkedPerson.ID != current || linkedPerson.Version != 1 || linkedPerson.LinkedUserID == nil || *linkedPerson.LinkedUserID != member {
		t.Fatalf("get member person=%+v err=%v", linkedPerson, err)
	}
	if _, err := service.GetMemberPerson(ctx, actor, home, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99"); !errors.Is(err, subject.ErrNotFound) {
		t.Fatalf("non-member get err=%v", err)
	}
	const foreignHome, foreignUser, foreignPerson = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b83"
	if _, err := admin.Exec(ctx, `INSERT INTO households(id,name,timezone) VALUES($1::uuid,'foreign home','UTC')`, foreignHome); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(id,login) VALUES($1::uuid,'foreign_member')`, foreignUser); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, foreignUser, foreignHome); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO subjects(id,household_id,type,name,linked_user_id) VALUES($1::uuid,$2::uuid,'person','Foreign person',$3::uuid)`, foreignPerson, foreignHome, foreignUser); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetMemberPerson(ctx, actor, home, foreignUser); !errors.Is(err, subject.ErrNotFound) {
		t.Fatalf("foreign household user get err=%v", err)
	}

	if _, err := admin.Exec(ctx, `INSERT INTO items(household_id,subject_id,title) VALUES($1::uuid,$2::uuid,'current item'),($1::uuid,$3::uuid,'target item')`, home, current, target); err != nil {
		t.Fatal(err)
	}
	result, err := service.Transfer(ctx, actor, home, member, subject.TransferRequest{
		SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1,
	})
	if err != nil || result.Person.ID != target || result.Previous.ID != current {
		t.Fatalf("transfer=%+v err=%v", result, err)
	}
	if result.Person.LinkedUserID == nil || *result.Person.LinkedUserID != member || result.Person.Version != 2 {
		t.Fatalf("new person=%+v", result.Person)
	}
	if result.Previous.LinkedUserID != nil || result.Previous.Version != 2 || result.Previous.Archived {
		t.Fatalf("previous=%+v", result.Previous)
	}
	afterTransfer, err := service.GetMemberPerson(ctx, actor, home, member)
	if err != nil || afterTransfer.ID != target || afterTransfer.Version != 2 || afterTransfer.LinkedUserID == nil || *afterTransfer.LinkedUserID != member {
		t.Fatalf("get member person after transfer=%+v err=%v", afterTransfer, err)
	}
	var itemLinks int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE (subject_id=$1::uuid OR subject_id=$2::uuid)`, current, target).Scan(&itemLinks); err != nil || itemLinks != 2 {
		t.Fatalf("item links=%d err=%v", itemLinks, err)
	}

	// Two transfers for the same member serialize on the membership row; the
	// second observes that the current person is no longer linked.
	reset()
	if _, err := admin.Exec(ctx, `INSERT INTO subjects(id,household_id,type,name) VALUES($1::uuid,$2::uuid,'person','Second target')`, target2, home); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, candidate := range []string{target, target2} {
		wg.Add(1)
		go func(candidate string) {
			defer wg.Done()
			_, err := service.Transfer(ctx, actor, home, member, subject.TransferRequest{
				SubjectID: candidate, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1,
			})
			results <- err
		}(candidate)
	}
	wg.Wait()
	close(results)
	successes, changed := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, subject.ErrMemberPersonChanged):
			changed++
		default:
			t.Fatalf("concurrent transfer err=%v", err)
		}
	}
	if successes != 1 || changed != 1 {
		t.Fatalf("concurrent transfers successes=%d changed=%d", successes, changed)
	}

	// Transfer racing a rename with the same version: exactly one commits.
	reset()
	var raceWG sync.WaitGroup
	raceWG.Add(2)
	raceErrs := make(chan error, 2)
	go func() {
		defer raceWG.Done()
		_, err := service.Transfer(ctx, actor, home, member, subject.TransferRequest{
			SubjectID: target, SubjectVersion: 1, CurrentSubjectID: current, CurrentSubjectVersion: 1,
		})
		raceErrs <- err
	}()
	go func() {
		defer raceWG.Done()
		name := "Renamed concurrently"
		_, err := service.Update(ctx, actor, home, current, 1, subject.Patch{Name: &name})
		raceErrs <- err
	}()
	raceWG.Wait()
	close(raceErrs)
	wins, stale := 0, 0
	for err := range raceErrs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, subject.ErrVersionMismatch):
			stale++
		default:
			t.Fatalf("race err=%v", err)
		}
	}
	if wins != 1 || stale != 1 {
		t.Fatalf("transfer/rename race wins=%d stale=%d", wins, stale)
	}
}
