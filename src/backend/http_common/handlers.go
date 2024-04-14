package http_common

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type hello struct {
	Name string `json:"name"`
}
type resposeMsg struct {
	Msg string `json:"msg"`
}

func getHello(c *gin.Context) {
	myHello := []hello{
		{Name: "Ashish"},
	}
	c.JSON(http.StatusOK, myHello)
}

func putHello(c *gin.Context){
	var reveicedHello hello
	err := c.BindJSON(&reveicedHello)
	if err != nil {
		return 
	}
	fmt.Printf("Recieved: %+v\n",reveicedHello)
	var respMsg resposeMsg
	respMsg.Msg = fmt.Sprintf("Hi %s",reveicedHello.Name) 
	c.JSON(http.StatusCreated,respMsg)
}