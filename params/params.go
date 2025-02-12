package params

import (
	"github.com/joho/godotenv"
	"log"
	"os"
)

type ParamName string

const (
	GIGACHAT_AUTH_DATA ParamName = "GIGACHAT_AUTH_DATA"
	COOKIES_PATH       ParamName = "COOKIES_PATH"
	COMPANY_ID         ParamName = "COMPANY_ID"
	GIGACHAT_PROMPT    ParamName = "GIGACHAT_PROMPT"
	API_KEY            ParamName = "API_KEY"
	CLIENT_ID          ParamName = "CLIENT_ID"
)

type Params map[ParamName]string

func LoadParams() Params {
	if err := godotenv.Load(); err != nil {
		log.Fatal(".env файл не найден")
	}
	var params = make(Params)
	paramNames := []ParamName{
		GIGACHAT_AUTH_DATA,
		COOKIES_PATH,
		COMPANY_ID,
		GIGACHAT_PROMPT,
		API_KEY,
		CLIENT_ID,
	}
	for _, paramName := range paramNames {
		value, exists := os.LookupEnv(string(paramName))
		if !exists {
			log.Fatalf("Параметр %s не задан", paramName)
		}
		params[paramName] = value
	}
	return params
}
