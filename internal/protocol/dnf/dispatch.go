package dnf

const invalidRequestMessage = "invalid request"

type DnfTableTaskResult struct {
	Msg  string
	Code int
}

type DnfTableDrive struct {
	task *RobotDnfTask
}

func NewDnfTableDrive() *DnfTableDrive {
	return &DnfTableDrive{task: NewRobotDnfTask()}
}

func (dt *DnfTableDrive) GetTask() *RobotDnfTask {
	return dt.task
}

func (dt *DnfTableDrive) Shutdown() {
	dt.task.Shutdown()
}
