package domain

const (
	TaskPriorityLow      = 1
	TaskPriorityNormal   = 2
	TaskPriorityHigh     = 3
	TaskPriorityUrgent   = 4
	TaskPriorityCritical = 5
)

var taskPriorityNames = [...]string{"", "low", "normal", "high", "urgent", "critical"}

func TaskPriorityFromName(name string) (int, bool) {
	for value := TaskPriorityLow; value <= TaskPriorityCritical; value++ {
		if taskPriorityNames[value] == name {
			return value, true
		}
	}
	return 0, false
}

func TaskPriorityName(value int) (string, bool) {
	if value < TaskPriorityLow || value > TaskPriorityCritical {
		return "", false
	}
	return taskPriorityNames[value], true
}

func ValidTaskPriorityName(name string) bool {
	_, ok := TaskPriorityFromName(name)
	return ok
}
