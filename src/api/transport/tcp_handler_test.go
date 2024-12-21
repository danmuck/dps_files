package transport

import (
	"fmt"
	"testing"
)

func Test_TCP_0(t *testing.T) {
	fmt.Println("Initializing new handler ...")
	listener := NewTCPHandler(":3000", nil)

	go listener.ListenAndAccept()

}
