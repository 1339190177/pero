package harness

func agentDisposalMaySelfWait(agent *Agent) bool {
	if agent == nil {
		return false
	}
	if agent.resourcePreparations.Load() != 0 || agent.operationCount.Load() != 0 {
		return true
	}
	if !agent.admissionMu.TryLock() {
		return true
	}
	agent.admissionMu.Unlock()
	if !agent.inboxClaimMu.TryLock() {
		return true
	}
	agent.inboxClaimMu.Unlock()
	if !agent.activityMu.TryLock() {
		return true
	}
	agent.activityMu.Unlock()
	return false
}
