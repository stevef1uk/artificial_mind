package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// AgentScheduler manages scheduled execution of agents
type AgentScheduler struct {
	registry    *AgentRegistry
	executor    *AgentExecutor
	cron        *cron.Cron
	runningJobs map[string]cron.EntryID // agentID -> entryID
	mutex       sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewAgentScheduler creates a new agent scheduler
func NewAgentScheduler(registry *AgentRegistry, executor *AgentExecutor) *AgentScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &AgentScheduler{
		registry:    registry,
		executor:    executor,
		cron:        cron.New(cron.WithSeconds()), // Support seconds in cron
		runningJobs: make(map[string]cron.EntryID),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start starts the scheduler and registers all scheduled agents
func (s *AgentScheduler) Start() error {
	log.Printf("⏰ [AGENT-SCHEDULER] Starting agent scheduler...")
	
	// Load all agents and register their schedules
	agentIDs := s.registry.ListAgents()
	scheduledCount := 0
	
	for _, agentID := range agentIDs {
		agent, ok := s.registry.GetAgent(agentID)
		if !ok {
			continue
		}
		
		// Register scheduled triggers
		if agent.Config.Triggers != nil && len(agent.Config.Triggers.Schedule) > 0 {
			for _, schedule := range agent.Config.Triggers.Schedule {
				if err := s.ScheduleAgent(agentID, schedule.Cron, schedule.Action); err != nil {
					log.Printf("⚠️ [AGENT-SCHEDULER] Failed to schedule agent %s: %v", agentID, err)
					continue
				}
				scheduledCount++
				log.Printf("✅ [AGENT-SCHEDULER] Scheduled agent %s with cron: %s (action: %s)", 
					agentID, schedule.Cron, schedule.Action)
			}
		}
	}
	
	// Start the cron scheduler
	s.cron.Start()
	log.Printf("✅ [AGENT-SCHEDULER] Scheduler started with %d scheduled agent(s)", scheduledCount)
	
	return nil
}

// ScheduleAgent schedules an agent to run on a cron schedule
func (s *AgentScheduler) ScheduleAgent(agentID string, cronExpr string, action string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	// Remove existing schedule for this agent if any
	if entryID, exists := s.runningJobs[agentID]; exists {
		s.cron.Remove(entryID)
		delete(s.runningJobs, agentID)
	}
	
	// Create a closure that captures agentID and action
	job := func() {
		log.Printf("⏰ [AGENT-SCHEDULER] Triggering scheduled execution: agent=%s, action=%s", agentID, action)
		
		// Create a context with timeout for agent execution
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		
		// Execute the agent with the action as input
		input := action
		if input == "" {
			input = "Execute scheduled task"
		}
		
		result, err := s.executor.ExecuteAgent(ctx, agentID, input)
		if err != nil {
			log.Printf("❌ [AGENT-SCHEDULER] Agent %s execution failed: %v", agentID, err)
			// Record execution failure in history
			s.recordExecution(agentID, input, nil, err)
			return
		}
		
		log.Printf("✅ [AGENT-SCHEDULER] Agent %s completed successfully", agentID)
		// Record execution success in history
		s.recordExecution(agentID, input, result, nil)
	}
	
	// Add cron job
	entryID, err := s.cron.AddFunc(cronExpr, job)
	if err != nil {
		return err
	}
	
	s.runningJobs[agentID] = entryID
	return nil
}

// UnscheduleAgent removes an agent from the schedule
func (s *AgentScheduler) UnscheduleAgent(agentID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if entryID, exists := s.runningJobs[agentID]; exists {
		s.cron.Remove(entryID)
		delete(s.runningJobs, agentID)
		log.Printf("⏰ [AGENT-SCHEDULER] Unscheduled agent: %s", agentID)
	}
}

// Stop stops the scheduler
func (s *AgentScheduler) Stop() {
	log.Printf("⏰ [AGENT-SCHEDULER] Stopping scheduler...")
	s.cancel()
	s.cron.Stop()
	log.Printf("✅ [AGENT-SCHEDULER] Scheduler stopped")
}

// IsScheduled checks if an agent is currently scheduled
func (s *AgentScheduler) IsScheduled(agentID string) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	_, exists := s.runningJobs[agentID]
	return exists
}

// recordExecution records an agent execution in history
func (s *AgentScheduler) recordExecution(agentID string, input string, result interface{}, err error) {
	// This will be implemented with execution history storage
	// For now, just log it
	if err != nil {
		log.Printf("📝 [AGENT-HISTORY] Agent %s execution failed: %v", agentID, err)
	} else {
		log.Printf("📝 [AGENT-HISTORY] Agent %s execution succeeded", agentID)
	}
}

