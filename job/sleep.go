package job

import (
	"log"
	"time"
)

func HandleSleep(data any) error {
	log.Println("Sleeping...")
	time.Sleep(data.(time.Duration))
	log.Println("Ready!")
	return nil
}
