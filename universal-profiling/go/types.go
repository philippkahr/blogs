package main

type Datastream struct {
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Dataset   string `json:"dataset"`
}
type Event struct {
	Ingested string `json:"ingested"`
}
type UserDetails struct {
	Name string `json:"name"`
	Elo  int    `json:"elo"`
	Diff int    `json:"diff"`
}
type User struct {
	White UserDetails `json:"white"`
	Black UserDetails `json:"black"`
}
type Opening struct {
	Eco  string `json:"eco"`
	Name string `json:"name"`
}
type Result struct {
	Outcome string `json:"outcome"`
	White   bool   `json:"white,omitempty"`
	Black   bool   `json:"black,omitempty"`
	Draw    bool   `json:"draw,omitempty"`
}
type Moves struct {
	Original string `json:"original"`
	Cleaned  string `json:"cleaned"`
}
type Source struct {
	Timestamp   string     `json:"@timestamp"`
	Db          string     `json:"db"`
	Event       Event      `json:"event"`
	Name        string     `json:"name"`
	Game_id     string     `json:"game_id"`
	Url         string     `json:"url"`
	Data_stream Datastream `json:"data_stream"`
	Moves       Moves      `json:"moves"`
	User        User       `json:"user"`
	Result      Result     `json:"result"`
	Opening     Opening    `json:"opening"`
	Termination string     `json:"termination"`
	Timecontrol string     `json:"timecontrol"`
}
type Doc struct {
	Index   string `json:"_index"`
	Op_type string `json:"_op_type"`
	Source  Source `json:"_source"`
}
