protoc --proto_path=api/proto/v1 --proto_path=third_party --go_out=plugins=grpc:pkg/api/v1 --js_out=import_style=commonjs:pkg/api/v1 --grpc-web_out=import_style=commonjs,mode=grpcwebtext:pkg/api/v1 todo-service.proto
protoc --proto_path=api/proto/v1 --proto_path=third_party --grpc-gateway_out=logtostderr=true:pkg/api/v1 --js_out=import_style=commonjs:pkg/api/v1 --grpc-web_out=import_style=commonjs,mode=grpcwebtext:pkg/api/v1 todo-service.proto
protoc --proto_path=api/proto/v1 --proto_path=third_party --swagger_out=logtostderr=true:api/swagger/v1 --js_out=import_style=commonjs:pkg/api/v1 --grpc-web_out=import_style=commonjs,mode=grpcwebtext:pkg/api/v1 todo-service.proto

# protoc --proto_path=api/proto/v1 --proto_path=third_party --js_out=import_style=commonjs:pkg/api/v1 --grpc-web_out=import_style=commonjs,mode=grpcwebtext:pkg/api/v1 todo-service.proto