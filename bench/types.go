package bench

// Minimal structural types for corpus decoding. We use interface{} for many
// corpus files to fairly exercise the generic decode path — that is the hot
// path most apps hit. Library benchmarks (sonic, goccy) all use the same
// interface{} / map[string]interface{} target for corpus payloads.

type SmallUser struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Age       int      `json:"age"`
	Active    bool     `json:"active"`
	Score     float64  `json:"score"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
}

type EmployeeData struct {
	Employees []Employee `json:"employees"`
}

type Employee struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Profile Profile `json:"profile"`
}

type Profile struct {
	Contact  Contact   `json:"contact"`
	Projects []Project `json:"projects"`
}

type Contact struct {
	Email   string  `json:"email"`
	Phone   string  `json:"phone"`
	Address Address `json:"address"`
}

type Address struct {
	Street   string   `json:"street"`
	City     string   `json:"city"`
	Location Location `json:"location"`
}

type Location struct {
	State   string   `json:"state"`
	Country string   `json:"country"`
	Geo     Geo      `json:"geo"`
}

type Geo struct {
	Lat      string   `json:"lat"`
	Long     string   `json:"long"`
	Timezone Timezone `json:"timezone"`
}

type Timezone struct {
	Name      string `json:"name"`
	UTCOffset string `json:"utc_offset"`
}

type Project struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Tasks     []Task `json:"tasks"`
}

type Task struct {
	TaskID      string     `json:"taskId"`
	Description string     `json:"description"`
	AssignedTo  AssignedTo `json:"assignedTo"`
}

type AssignedTo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Skills Skills `json:"skills"`
}

type Skills struct {
	Primary    string     `json:"primary"`
	Secondary  []string   `json:"secondary"`
	Experience Experience `json:"experience"`
}

type Experience struct {
	Years          int            `json:"years"`
	Domains        []string       `json:"domains"`
	Certifications Certifications `json:"certifications"`
}

type Certifications struct {
	Current []string       `json:"current"`
	Expired []string       `json:"expired"`
	Meta    Meta           `json:"meta"`
}

type Meta struct {
	Verified    bool   `json:"verified"`
	LastUpdated string `json:"lastUpdated"`
}
