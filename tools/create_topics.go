package main

import (
	"fmt"
	"log"
	"time"

	"github.com/IBM/sarama"
)

func main() {
	brokers := []string{"localhost:9092"}
	config := sarama.NewConfig()
	config.Version = sarama.V2_5_0_0 // Adjust based on your Kafka version
	admin, err := sarama.NewClusterAdmin(brokers, config)
	if err != nil {
		log.Fatalf("Error creating cluster admin: %v", err)
	}
	defer admin.Close()

	topics := []string{"product-events", "price-updates"}
	for _, topic := range topics {
		err := admin.CreateTopic(topic, &sarama.TopicDetail{
			NumPartitions:     1,
			ReplicationFactor: 1,
		}, false)
		if err != nil {
			log.Printf("Error creating topic %s: %v", topic, err)
		} else {
			fmt.Printf("Topic created: %s\n", topic)
		}
	}

	time.Sleep(2 * time.Second)
}
