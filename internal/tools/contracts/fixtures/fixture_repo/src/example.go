package fixture

import "fmt"

const SearchToken = "EXAMPLE_SEARCH_TOKEN"

func ExampleMessage(name string) string {
	return fmt.Sprintf("hello, %s", name)
}

func ExampleSearchTarget() string {
	return "alpha-123-beta"
}
