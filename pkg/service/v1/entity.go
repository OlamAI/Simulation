package v1

import (
	"errors"
)

// Entity - data for entities that exist in cells
type Entity struct {
	id        int64
	class     string
	pos       Vec2
	energy    int32
	health    int32
	ownerUID  string
	modelName string
}

const initialEnergy = 100
const initialHealth = 100

// Given an entity, it will perform all living cost calculations, AND remove
//  the entity if it dies returning whether or not it is still alive.
func (s *simulationServiceServer) agentLivingCost(a *Entity) (isStilAlive bool) {
	// Lower health immediatly if energy is 0
	if a.energy == 0 {
		a.health -= 10
	}
	// Kill the agent if they have no health and end call
	if a.health <= 0 {
		s.removeEntityByID(a.id)
		return false
	}
	// Take away energy
	a.energy -= agentLivingEnergyCost
	if a.energy < 0 {
		a.energy = 0
	}
	return true
}

// Check to see if an entity still exists by ID
func (s *simulationServiceServer) doesEntityExist(id int64) bool {
	_, ok := s.entities[id]
	// Return false if an entitiy by that id doesn't exist
	return ok
}

// Create a new entity and add it to the simulation
func (s *simulationServiceServer) newEntity(class string, ownerUID string, modelName string, pos Vec2) *Entity {
	// Create the entity
	id := s.nextEntityID
	s.nextEntityID++
	e := Entity{id, class, pos, initialEnergy, initialHealth, ownerUID, modelName}
	s.entities[id] = &e
	s.posEntityMap[pos] = &e

	// Add to agents map if it is an agent
	if class == "AGENT" {
		s.agents[id] = &e
	}

	// Broadcast update
	s.broadcastCellUpdate(e.pos, &e)

	return &e
}

// Creates a new agent. Agents are just an abstraction of an entity, essentially
//  just a group of entities.
func (s *simulationServiceServer) newAgent(ownerUID string, modelName string, pos Vec2) (*Entity, error) {
	if s.env == "prod" {
		if !s.doesRemoteModelExist(ownerUID, modelName) {
			return nil, errors.New("CreateNewEntity(): That model does not exist")
		}
	}

	return s.newEntity("AGENT", ownerUID, modelName, pos), nil
}

// Remove an entity by Id and broadcast the update
func (s *simulationServiceServer) removeEntityByID(id int64) bool {
	// Get the entitiy
	e, ok := s.entities[id]
	// Return false if an entitiy by that id doesn't exist
	if !ok {
		return false
	}
	// Remove from agents map if it is an agent
	if e.class == "AGENT" {
		delete(s.agents, e.id)
	}
	// Remove the entity
	delete(s.entities, e.id)
	delete(s.posEntityMap, e.pos)
	// Broadcast update
	s.broadcastCellUpdate(e.pos, nil)

	return true
}

// Move an entity
func (s *simulationServiceServer) entityMove(id int64, targetPos Vec2) bool {
	// Get the entity by id
	e, ok := s.entities[id]

	// [Start Checks]
	// Make sure the entity exists
	if !ok {
		return false
	}
	// Make sure space is empty
	if _, ok := s.posEntityMap[targetPos]; ok {
		return false
	}
	// [End Checks]

	// Send to observation
	s.broadcastCellUpdate(e.pos, nil)
	// Remove entity from current position
	delete(s.posEntityMap, e.pos)

	// Move the entity to new position
	e.pos = targetPos
	s.posEntityMap[targetPos] = e
	// Send to observation
	s.broadcastCellUpdate(e.pos, e)

	return true
}

// Entity will consume another cell's coccupant
func (s *simulationServiceServer) entityConsume(id int64, targetPos Vec2) bool {
	// Get the entity by id
	e, ok := s.entities[id]

	// [Start Checks]
	// Make sure the entity exists
	if !ok {
		return false
	}
	// Make sure space is empty
	targetEntity, ok := s.posEntityMap[targetPos]
	if !ok {
		return false
	}
	// Target entity was found, make sure class is consumable
	if targetEntity.class != "FOOD" {
		return false
	}
	// [End Checks]

	// Remove food entity
	s.removeEntityByID(targetEntity.id)
	// Add to current entity's energy
	e.energy += 10

	return true
}

// Checks if a cell is currently occupied
func (s *simulationServiceServer) isCellOccupied(pos Vec2) bool {
	if _, ok := s.posEntityMap[pos]; ok {
		return true
	}
	return false
}
