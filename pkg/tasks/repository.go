package tasks

type Repository interface {
	NextID() (int, error)
	Save(task Task) error
	Get(id int) (Task, error)
	List() ([]Task, error)
}
