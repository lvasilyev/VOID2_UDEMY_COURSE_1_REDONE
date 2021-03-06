package main

import(
	//Native Packages
	"fmt"
	"flag"
	"time"
	"strings"
	"net/http"
	"io/ioutil"
	"encoding/json"
	
	"github.com/google/uuid"
	
	//3rd Party Packages
	"github.com/gorilla/mux"
	//"github.com/gorilla/handlers"
	"github.com/gorilla/websocket"
	
	//Our Packages
	"procon_jwt"
	"procon_pty"
	"procon_data"
	"procon_utils"
	"procon_mongo"
	"procon_mysql"
	"procon_config"
	"procon_asyncq"
	"procon_filesystem"
)

var addr = flag.String("addr", "0.0.0.0:1200", "http service address")
var upgrader = websocket.Upgrader{} // use default options


type WsClients struct{
	CC int
	CIDS []string
}

var Table chan *WsClients;




func handleAPI(w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Print("WTF @HandleAPI Ws Upgrade Error> ", err)
		return
	}
	
	id, err := uuid.NewRandom() 
	if err != nil { fmt.Println(err) }
	
	//Modified Mux Websocket package Conn Struct in Conn.go
	c.Uuid = "ws-"+id.String()		
	
	go func() {
		//take control of WsClients pointer from channel
		wscc := <- Table
		wscc.CC++
		wscc.CIDS = append(wscc.CIDS, c.Uuid)
		
		fmt.Println(wscc);
		
		Table <- wscc
	}()
		
	
	go func(Table chan *WsClients, c *websocket.Conn) {
		for range time.Tick(time.Second * 5) {
			wscc := <- Table
			mcl, err := json.Marshal(wscc)
			if err != nil { fmt.Println(err) } else {
				procon_data.SendMsg("^vAr^", "websocket-client-list", string(mcl), c);
			}
			
			Table <- wscc					
		}	
	}(Table, c)
	
	
	
	Loop:
		for {
			in := procon_data.Msg{}
			
			err := c.ReadJSON(&in)
			if err != nil {
				
				c.Close()
				break Loop
			}	
			switch(in.Type) {
				case "register-client-msg":
					procon_data.SendMsg("^vAr^", "server-ws-connect-success-msg", c.Uuid , c);						
					break;
				case "create-user":
					res := procon_mongo.CreateUser(in.Data, c)
					fmt.Println("Mongo Function Result: ", res)
					//Change Role in mongo package!!!!	
					break;
				case "login-user":
					usr, pwd, err := procon_utils.B64DecodeTryUser(in.Data);
					if err != nil { fmt.Println(err);  } else {   	
						vres, auser, err := procon_mongo.MongoTryUser(usr,pwd);
						if err != nil { fmt.Println(err) } else if vres == true {
							auser.Password = "F00"
							
							jauser,err := json.Marshal(auser); if err != nil { fmt.Println("error marshaling AUser.") }else {
								jwt, err := procon_jwt.GenerateJWT(procon_config.PrivKeyFile, auser.Name, "You Implement", auser.Email, auser.Role)
								if err != nil { fmt.Println(err); } else {  procon_data.SendMsg(jwt, "server-ws-connect-success-jwt", string(jauser), c ); }
							}	
						}
						if vres == false {
							procon_data.SendMsg("^vAr^", "server-ws-connect-login-failure", "User Not Found or Invalid Credentials", c );
						}
					}
				case "validate-jwt": fallthrough
				case "validate-stored-jwt":
					valid, err := procon_jwt.ValidateJWT(procon_config.PubKeyFile, in.Jwt)
					fmt.Println(in.Jwt);
					if err != nil  {  
						fmt.Println(err); 
						if in.Type == "validate-jwt" { procon_data.SendMsg("^vAr^", "jwt-token-invalid", err.Error(), c) }
						if in.Type == "validate-stored-jwt" { procon_data.SendMsg("^vAr^", "stored-jwt-token-invalid", err.Error(), c) }
					} else if (err == nil && valid) {
						if in.Type == "validate-jwt" {  procon_data.SendMsg("^vAr^", "server-ws-connect-jwt-verified", "noop", c); }
						if in.Type == "validate-stored-jwt" {  procon_data.SendMsg("^vAr^", "server-ws-connect-stored-jwt-verified", "noop", c); }
					}
				
				case "get-fs-path":
					fmt.Println(in.Data)
				
					if strings.HasPrefix(in.Data, "/var/www/VFS/")  {
						tobj :=  procon_filesystem.NewGetFileSystemTask(in.Data, c);
						procon_asyncq.TaskQueue <- tobj	
					}
					break;
				case "return-fs-path-data":
					data, err := ioutil.ReadFile(in.Data);
					if err != nil { fmt.Println(err); } else {
						procon_data.SendMsg("vAr","rtn-file-data", string(data), c);
					}
					break;
					
				//Operations...	
				case "get-mysql-databases":
					tobj := procon_mysql.NewGetMysqlDbsTask(c);
					procon_asyncq.TaskQueue <- tobj					
					break;		

													
				default:
					break;					
			}
		}		
}

func handleUI(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	component := params["component"]

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json");
	fmt.Println(component);

	procon_mongo.MongoGetUIComponent(component, w)	
}


func main() {
	flag.Parse()
	procon_asyncq.StartTaskDispatcher(9)
	
	//look into subrouter stuffs
	r := mux.NewRouter()	
	
	//Websocket API
	r.HandleFunc("/ws", handleAPI)
	r.HandleFunc("/pty", procon_pty.HandlePty)
	
	//Rest API
	r.HandleFunc("/rest/api/ui/{component}", handleUI)
	

	go func() {
		Table = make(chan *WsClients);
		Table <- new(WsClients)		
	}()
	
	//Rest API
	http.ListenAndServeTLS(*addr,"/etc/letsencrypt/live/void.pr0con.com/cert.pem", "/etc/letsencrypt/live/void.pr0con.com/privkey.pem", r)			
}