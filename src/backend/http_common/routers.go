package http_common

import "github.com/gin-gonic/gin"


func AddRoutes(engine *gin.Engine) *gin.RouterGroup {
	group := engine.Group("/")
	for _, route := range routes {
		switch route.Method {
		case "GET":
			group.GET(route.Pattern, route.HandlerFunc)
		case "POST":
			group.POST(route.Pattern, route.HandlerFunc)
		case "PUT":
			group.PUT(route.Pattern, route.HandlerFunc)
		case "DELETE":
			group.DELETE(route.Pattern, route.HandlerFunc)
		}
	}
	return group
}

type Route struct {
	// Name is the name of this Route.
	Name string
	// Method is the string for the HTTP method. ex) GET, POST etc..
	Method string
	// Pattern is the pattern of the URI.
	Pattern string
	// HandlerFunc is the handler function of this route.
	HandlerFunc gin.HandlerFunc
}

type Routes []Route

var routes = Routes {
	{
		"TestPostRoute",
		"POST",
		"/sayHello",
		putHello,
	},
	{
		"TestGetRoute",
		"GET",
		"/hello",
		getHello,
	},
}
