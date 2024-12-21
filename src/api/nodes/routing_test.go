package nodes

import (
	"fmt"
	"testing"
	"time"

	"math/rand"
)

func GenerateKey() []byte {
	i := 20
	b := make([]byte, 0, i)
	for i > 0 {
		i--
		r := rand.Intn(256)
		b = append(b, byte(r))
	}

	return b
}
func TestNewRouter(t *testing.T) {
	n, err := NewDefaultNode(GenerateKey(), "localhost:3000", 5, 5)
	if err != nil {
		t.Error(err)
	}
	r := n.Router
	fmt.Printf("Router: %+v \n", r)

	go n.Start()

	time.Sleep(30 * time.Second)
}
