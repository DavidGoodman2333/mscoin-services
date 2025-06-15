package market

import "testing"

func TestMathSuite(t *testing.T) {
    tests := []struct {
        name string
        a, b int
        want int
    }{
        {"1+1", 1, 1, 2},
        {"2+3", 2, 3, 5},
        {"-1+1", -1, 1, 0},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := tt.a + tt.b
            if got != tt.want {
                t.Errorf("got %d, want %d", got, tt.want)
            }
        })
    }
}
