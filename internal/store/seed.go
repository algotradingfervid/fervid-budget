package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"fervidbudget/internal/password"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultSeedAdminEmail = "admin@fervid.local"
	defaultSeedAdminName  = "Fervid Admin"
	defaultSeedMonth      = "2026-06"
)

type SeedOptions struct {
	AdminEmail        string
	AdminName         string
	AdminPassword     string
	AdminPasswordHash string
	Month             string
}

type SeedResult struct {
	AdminCreated  bool
	SampleCreated bool
	SampleSkipped bool
	Projects      int
	Heads         int
	Budgets       int
}

func (s *Store) Seed(ctx context.Context, opts SeedOptions) (SeedResult, error) {
	opts = normalizeSeedOptions(opts)
	var result SeedResult

	admin, created, err := s.ensureSeedAdmin(ctx, opts)
	if err != nil {
		return result, err
	}
	result.AdminCreated = created

	empty, err := s.sampleDataEmpty(ctx)
	if err != nil {
		return result, err
	}
	if !empty {
		result.SampleSkipped = true
		return result, nil
	}

	projects, heads, budgets, err := s.createSampleBudgetData(ctx, admin, opts.Month)
	if err != nil {
		return result, err
	}
	result.SampleCreated = true
	result.Projects = projects
	result.Heads = heads
	result.Budgets = budgets
	return result, nil
}

func (s *Store) SeedSampleData(ctx context.Context, month string) (SeedResult, error) {
	return s.Seed(ctx, SeedOptions{Month: month})
}

func normalizeSeedOptions(opts SeedOptions) SeedOptions {
	opts.AdminEmail = strings.ToLower(strings.TrimSpace(opts.AdminEmail))
	if opts.AdminEmail == "" {
		opts.AdminEmail = defaultSeedAdminEmail
	}
	opts.AdminName = strings.TrimSpace(opts.AdminName)
	if opts.AdminName == "" {
		opts.AdminName = defaultSeedAdminName
	}
	if !validMonth(opts.Month) {
		opts.Month = defaultSeedMonth
	}
	return opts
}

func (s *Store) ensureSeedAdmin(ctx context.Context, opts SeedOptions) (User, bool, error) {
	existing, err := s.UserByEmail(ctx, opts.AdminEmail)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, false, err
	}

	hash := strings.TrimSpace(opts.AdminPasswordHash)
	if hash == "" {
		if err := password.Validate(opts.AdminPassword); err != nil {
			return User{}, false, fmt.Errorf("bootstrap password: %w", err)
		}
		generated, err := bcrypt.GenerateFromPassword([]byte(opts.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return User{}, false, err
		}
		hash = string(generated)
	}
	if err := s.EnsureUser(ctx, opts.AdminEmail, opts.AdminName, hash, "admin"); err != nil {
		return User{}, false, err
	}
	admin, err := s.UserByEmail(ctx, opts.AdminEmail)
	return admin, true, err
}

func (s *Store) sampleDataEmpty(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM projects) +
		(SELECT COUNT(*) FROM heads) +
		(SELECT COUNT(*) FROM budgets)`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *Store) createSampleBudgetData(ctx context.Context, actor User, month string) (int, int, int, error) {
	if actor.ID == 0 {
		return 0, 0, 0, fmt.Errorf("%w: seed actor is required", ErrValidation)
	}
	projectCount := 0
	headCount := 0
	budgetCount := 0
	for i, project := range sampleProjects {
		projectID, err := s.UpsertProject(ctx, 0, project.Name, true, i+1)
		if err != nil {
			return projectCount, headCount, budgetCount, err
		}
		projectCount++
		for j, head := range project.Heads {
			headID, err := s.UpsertHead(ctx, 0, projectID, head.Name, head.DueDay, true, j+1)
			if err != nil {
				return projectCount, headCount, budgetCount, err
			}
			headCount++
			if err := s.SetBudget(ctx, actor, headID, month, head.Budget); err != nil {
				return projectCount, headCount, budgetCount, err
			}
			budgetCount++
		}
	}
	return projectCount, headCount, budgetCount, nil
}

type sampleProject struct {
	Name  string
	Heads []sampleHead
}

type sampleHead struct {
	Name   string
	DueDay string
	Budget int64
}

var sampleProjects = []sampleProject{
	{
		Name: "Operations",
		Heads: []sampleHead{
			{Name: "Office Rent", DueDay: "05", Budget: 25000000},
			{Name: "Utilities", DueDay: "10", Budget: 4500000},
			{Name: "Office Supplies", DueDay: "15", Budget: 1750000},
		},
	},
	{
		Name: "People",
		Heads: []sampleHead{
			{Name: "Payroll", DueDay: "01", Budget: 85000000},
			{Name: "Contractor Fees", DueDay: "12", Budget: 18000000},
			{Name: "Staff Welfare", DueDay: "20", Budget: 3000000},
		},
	},
	{
		Name: "Growth",
		Heads: []sampleHead{
			{Name: "Marketing", DueDay: "08", Budget: 12000000},
			{Name: "Travel", DueDay: "18", Budget: 6500000},
			{Name: "Events", DueDay: "25", Budget: 5000000},
		},
	},
}
