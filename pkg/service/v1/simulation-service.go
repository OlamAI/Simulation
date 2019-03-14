package v1

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	firebase "firebase.google.com/go"

	v1 "github.com/olamai/simulation/pkg/api/v1"
	"github.com/olamai/simulation/pkg/logger"
)

const (
	// apiVersion is version of API is provided by server
	apiVersion            = "v1"
	agentLivingEnergyCost = 2
)

// toDoServiceServer is implementation of v1.ToDoServiceServer proto interface
type simulationServiceServer struct {
	// Environment the server is running in
	env string
	// Entity storage
	nextEntityID int64
	entities     map[int64]*Entity
	// Map from position -> *Entity
	posEntityMap map[Vec2]*Entity
	// Map from spectator id -> observation channel
	spectIDChanMap map[string]chan v1.SpectateResponse
	// Specators subscription to regions
	spectRegionSubs map[Vec2][]string
	// Firebase app
	firebaseApp *firebase.App
	// Mutex to ensure data safety
	m sync.Mutex
}

// NewSimulationServiceServer creates ToDo service
func NewSimulationServiceServer(env string) v1.SimulationServiceServer {
	s := &simulationServiceServer{
		env:             env,
		entities:        make(map[int64]*Entity),
		posEntityMap:    make(map[Vec2]*Entity),
		spectIDChanMap:  make(map[string]chan v1.SpectateResponse),
		spectRegionSubs: make(map[Vec2][]string),
		firebaseApp:     initializeFirebaseApp(env),
	}

	if env != "testing" {
		// Spawn food randomly
		for i := 0; i < 100; i++ {
			x := int32(rand.Intn(50) - 25)
			y := int32(rand.Intn(50) - 25)
			// Don't put anything at 0,0
			if x == 0 || y == 0 {
				continue
			}
			s.newEntity("FOOD", Vec2{x, y})
		}
	}

	return s
}

// checkAPI checks if the API version requested by client is supported by server
func (s *simulationServiceServer) checkAPI(api string) error {
	// API version is "" means use current version of the service
	if len(api) > 0 {
		if apiVersion != api {
			return status.Errorf(codes.Unimplemented,
				"unsupported API version: service implements API version '%s', but asked for '%s'", apiVersion, api)
		}
	}
	return nil
}

// Create new agent
func (s *simulationServiceServer) CreateAgent(ctx context.Context, req *v1.CreateAgentRequest) (*v1.CreateAgentResponse, error) {
	// Lock the data, defer unlock until end of call
	s.m.Lock()
	defer s.m.Unlock()
	// Check if the API version requested by client is supported by server
	if err := s.checkAPI(req.Api); err != nil {
		return nil, err
	}

	// Verify the auth token
	token := verifyFirebaseIDToken(ctx, s.firebaseApp, s.env)
	if token == nil {
		err := errors.New("CreateAgent(): Unable to verify auth token")
		return nil, err
	}

	// Get pos the user is trying to spawn an agent in
	targetPos := Vec2{req.X, req.Y}

	// Make sure the cell is empty
	if s.isCellOccupied(targetPos) {
		err := errors.New("CreateAgent(): Cell is already occupied")
		return nil, err
	}

	// Create a new agent (which is an entity)
	agent := s.newEntity("AGENT", Vec2{req.X, req.Y})

	return &v1.CreateAgentResponse{
		Api: apiVersion,
		Id:  agent.id,
	}, nil
}

// Get data for an entity
func (s *simulationServiceServer) GetEntity(ctx context.Context, req *v1.GetEntityRequest) (*v1.GetEntityResponse, error) {
	// Lock the data, defer unlock until end of call
	s.m.Lock()
	defer s.m.Unlock()
	// check if the API version requested by client is supported by server
	if err := s.checkAPI(req.Api); err != nil {
		return nil, err
	}
	// Get the entity from the map
	entity, ok := s.entities[req.Id]
	// Throw an error if an agent by that id doesn't exist
	if !ok {
		err := errors.New("GetEntity(): Entity Not Found")
		return nil, err
	}

	// Return the data for the agent
	return &v1.GetEntityResponse{
		Api: apiVersion,
		Entity: &v1.Entity{
			Id:    entity.id,
			Class: entity.class,
		},
	}, nil
}

// Remove an agent
func (s *simulationServiceServer) DeleteAgent(ctx context.Context, req *v1.DeleteAgentRequest) (*v1.DeleteAgentResponse, error) {
	// Lock the data, defer unlock until end of call
	s.m.Lock()
	defer s.m.Unlock()
	// check if the API version requested by client is supported by server
	if err := s.checkAPI(req.Api); err != nil {
		return nil, err
	}
	// Verify the auth token
	token := verifyFirebaseIDToken(ctx, s.firebaseApp, s.env)
	if token == nil {
		err := errors.New("CreateAgent(): Unable to verify auth token")
		return nil, err
	}

	// Get the agent
	agent, ok := s.entities[req.Id]
	// Throw an error if an agent by that id doesn't exist
	if !ok {
		err := errors.New("GetAgent(): Agent Not Found")
		return nil, err
	}

	// Remove the entity
	s.removeEntityByID(agent.id)

	// Return the data for the agent
	return &v1.DeleteAgentResponse{
		Api:     apiVersion,
		Deleted: 1,
	}, nil
}

// Execute an action for an agent
func (s *simulationServiceServer) ExecuteAgentAction(ctx context.Context, req *v1.ExecuteAgentActionRequest) (*v1.ExecuteAgentActionResponse, error) {
	// Lock the data, defer unlock until end of call
	s.m.Lock()
	defer s.m.Unlock()
	// Get data from request
	action := req.Action
	var actionSuccess bool
	// check if the API version requested by client is supported by server
	if err := s.checkAPI(req.Api); err != nil {
		return nil, err
	}
	// Get the agent
	agent, ok := s.entities[req.Id]
	// Throw an error if an agent by that id doesn't exist
	if !ok {
		err := errors.New("GetAgent(): Agent Not Found")
		return nil, err
	}
	// Lower health immediatly if energy is 0
	if agent.energy == 0 {
		agent.health -= 10
	}
	// Kill the agent if they have no health and end call
	if agent.health <= 0 {
		s.removeEntityByID(agent.id)
		return &v1.ExecuteAgentActionResponse{
			Api:                 apiVersion,
			IsAgentStillAlive:   false,
			WasActionSuccessful: false,
		}, nil
	}
	// Get the target position from the given direction
	var targetPos Vec2
	switch action.Direction {
	case "UP":
		targetPos = Vec2{agent.pos.x, agent.pos.y + 1}
	case "DOWN":
		targetPos = Vec2{agent.pos.x, agent.pos.y - 1}
	case "LEFT":
		targetPos = Vec2{agent.pos.x - 1, agent.pos.y}
	case "RIGHT":
		targetPos = Vec2{agent.pos.x + 1, agent.pos.y}
	default: // Direction not correct
		return nil, errors.New("ExecuteAgentAction(): Invalid Action.Direction")
	}

	// Perform the action
	switch action.Id {
	case "MOVE":
		actionSuccess = s.entityMove(agent.id, targetPos)
	case "CONSUME":
		actionSuccess = s.entityConsume(agent.id, targetPos)
	}

	// Take off living expense
	agent.energy -= agentLivingEnergyCost
	if agent.energy < 0 {
		agent.energy = 0
	}

	return &v1.ExecuteAgentActionResponse{
		Api:                 apiVersion,
		IsAgentStillAlive:   true,
		WasActionSuccessful: actionSuccess,
	}, nil
}

// Get an observation for an agent
func (s *simulationServiceServer) GetAgentObservation(ctx context.Context, req *v1.GetAgentObservationRequest) (*v1.GetAgentObservationResponse, error) {
	// Lock the data, defer unlock until end of call
	s.m.Lock()
	defer s.m.Unlock()
	// Get the agent
	e, ok := s.entities[req.Id]

	if ok {
		cells := s.getObservationCellsForPosition(e.pos)
		// Agent is alive and well... maybe, at least it's alive
		return &v1.GetAgentObservationResponse{
			Api: apiVersion,
			Observation: &v1.Observation{
				Alive:  true,
				Cells:  cells,
				Energy: e.energy,
				Health: e.health,
			},
		}, nil
	}
	// Agent doesn't exist anymore
	return &v1.GetAgentObservationResponse{
		Api: apiVersion,
		Observation: &v1.Observation{
			Alive:  false,
			Cells:  []string{},
			Energy: 0,
			Health: 0,
		},
	}, nil
}

// Remove an agent
func (s *simulationServiceServer) CreateSpectator(req *v1.CreateSpectatorRequest, stream v1.SimulationService_CreateSpectatorServer) error {
	// Lock the data, unlock after spectator is added
	s.m.Lock()
	// Get spectator ID from client in the request
	spectatorID := req.Id
	s.addSpectatorChannel(spectatorID)
	// Unlock data
	s.m.Unlock()

	// Listen for updates and send them to the client
	for {
		response := <-s.spectIDChanMap[spectatorID]
		if err := stream.Send(&response); err != nil {
			// Break the sending loop
			break
		}
	}

	// Remove the spectator and clean up
	// Lock data until spectator is removed
	s.m.Lock()
	s.removeSpectatorChannel(spectatorID)
	// Unlock data
	s.m.Unlock()
	log.Printf("Spectator left...")

	return nil
}

// Get an observation for an agent
func (s *simulationServiceServer) SubscribeSpectatorToRegion(ctx context.Context, req *v1.SubscribeSpectatorToRegionRequest) (*v1.SubscribeSpectatorToRegionResponse, error) {
	// Lock the data while creating the spectator
	s.m.Lock()
	// customHeader := ctx.Value("custom-header=1")
	id := req.Id
	region := Vec2{req.Region.X, req.Region.Y}
	// If the user is already subbed, successful is false
	if s.isSpectatorAlreadySubscribedToRegion(id, region) {
		return &v1.SubscribeSpectatorToRegionResponse{
			Api:        apiVersion,
			Successful: false,
		}, nil
	}
	// Add spectator id to subscription slice
	s.spectRegionSubs[region] = append(s.spectRegionSubs[region], id)
	// Get spectator channel
	channel := s.spectIDChanMap[id]
	// Unlock the data
	s.m.Unlock()

	// If the channel hasn't been created yet, try waiting a couple seconds then trying again
	//  Try this 3 times
	for i := 1; i < 4; i++ {
		if channel != nil {
			break
		}
		logger.Log.Warn("SubscribeSpectatorToRegion(): Spectator channel is nil, sleeping and trying again. Try #" + string(i))
		time.Sleep(2 * time.Second)
		// Lock the data when attempting to read from spect map
		s.m.Lock()
		channel = s.spectIDChanMap[id]
		// Unlock the data
		s.m.Unlock()
	}
	// If after the retrys it still hasn't found a channel throw an error
	if channel == nil {
		return nil, errors.New("SubscribeSpectatorToRegion(): Couldn't find a spectator by that id")
	}

	// Lock the data while sending the spectator the initial region data
	s.m.Lock()
	defer s.m.Unlock()

	// Send initial world state
	xs, ys := region.getPositionsInRegion()
	for _, x := range xs {
		for _, y := range ys {
			pos := Vec2{x, y}
			if entity, ok := s.posEntityMap[pos]; ok {
				channel <- v1.SpectateResponse{
					Data: &v1.SpectateResponse_CellUpdate{
						&v1.CellUpdate{
							X: pos.x,
							Y: pos.y,
							Entity: &v1.Entity{
								Id:    entity.id,
								Class: entity.class,
							},
						},
					},
				}
			}
		}
	}

	return &v1.SubscribeSpectatorToRegionResponse{
		Api:        apiVersion,
		Successful: true,
	}, nil
}

func (s *simulationServiceServer) ResetWorld(ctx context.Context, req *v1.ResetWorldRequest) (*v1.ResetWorldResponse, error) {
	// Lock the data, defer unlock until end of call
	s.m.Lock()
	defer s.m.Unlock()
	// check if the API version requested by client is supported by server
	if err := s.checkAPI(req.Api); err != nil {
		return nil, err
	}
	// Verify the auth token
	token := verifyFirebaseIDToken(ctx, s.firebaseApp, s.env)
	if token == nil {
		err := errors.New("ResetWorld(): Unable to verify auth token")
		return nil, err
	}

	s.entities = make(map[int64]*Entity)
	s.posEntityMap = make(map[Vec2]*Entity)
	// Spawn food randomly
	for i := 0; i < 100; i++ {
		x := int32(rand.Intn(50) - 25)
		y := int32(rand.Intn(50) - 25)
		// Don't put anything at 0,0
		if x == 0 || y == 0 {
			continue
		}
		s.newEntity("FOOD", Vec2{x, y})
	}
	// Broadcast the reset
	s.broadcastServerAction("RESET")
	// Broadcast new cells
	for pos, e := range s.posEntityMap {
		s.broadcastCellUpdate(pos, e)
	}
	// Return
	return &v1.ResetWorldResponse{}, nil
}
