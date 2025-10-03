package selfmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const redisAddr = "localhost:6379"
const redisKey = "selfmodel:test"

func setupManager() *Manager {
	m := NewManager(redisAddr, redisKey)
	// Clean slate
	_ = m.client.Del(ctx, redisKey).Err()
	return m
}

func TestBeliefUpdate(t *testing.T) {
	m := setupManager()
	err := m.UpdateBelief("weather", "sunny")
	assert.NoError(t, err)

	sm, err := m.Load()
	assert.NoError(t, err)
	assert.Equal(t, "sunny", sm.Beliefs["weather"])
}

func TestGoalLifecycle(t *testing.T) {
	m := setupManager()
	err := m.AddGoal("learn matrix ops")
	assert.NoError(t, err)

	sm, _ := m.Load()
	assert.Equal(t, 1, len(sm.Goals))
	goal := sm.Goals[0]

	err = m.UpdateGoalStatus(goal.ID, "completed")
	assert.NoError(t, err)

	sm2, _ := m.Load()
	assert.Equal(t, "completed", sm2.Goals[0].Status)
}

func TestRecordEpisode(t *testing.T) {
	m := setupManager()
	err := m.RecordEpisode("prime task", "use cached", "success", true, map[string]string{"note": "fast"})
	assert.NoError(t, err)

	sm, _ := m.Load()
	assert.Equal(t, 1, len(sm.History))
	assert.Equal(t, "prime task", sm.History[0].Event)
	assert.True(t, sm.History[0].Success)
}

