package main
 
import (
    "database/sql"
    "fmt"
    "html/template"
    "log"
    "math"
    "net/http"
    "strconv"
    "sync"
    "time"
 
 mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/gin-gonic/gin"
 _ "github.com/go-sql-driver/mysql"
    "github.com/gorilla/sessions"
    "golang.org/x/crypto/bcrypt"
)
 
var mu sync.Mutex
 
var store = sessions.NewCookieStore([]byte("secret"))
 
var broker = "77.222.37.139:1883"
 
type Device struct {
    id                int
    name              string
    gas_topic         string
    humidity_topic    string
    temperature_topic string
    gas_value         float64
    humidity_value    float64
    temperature_value float64
}
 
type User struct {
    Id    int
    Login string
}
 
func handleDataUpdate(client mqtt.Client, msg mqtt.Message, device *Device, mu *sync.Mutex, db *sql.DB, topic string) {
    time.Sleep(5 * time.Second)
    payload := string(msg.Payload())
    value, err := strconv.ParseFloat(payload, 64)
    if err != nil {
        fmt.Println("Error parsing value:", err)
        return
    }
 
    if math.IsNaN(value) {
        fmt.Println("Получено значение NaN")
        return
    }
 
    mu.Lock()
    defer mu.Unlock()
 
    switch topic {
    case device.gas_topic:
        device.gas_value = value
    case device.humidity_topic:
        humidity, err := strconv.ParseFloat(payload, 64)
        if err != nil {
            fmt.Println("Error parsing humidity:", err)
            return
        }
        if humidity < 0 || humidity > 100 {
            fmt.Println("Received invalid humidity value:", humidity)
            return
        }
        device.humidity_value = humidity
    case device.temperature_topic:
        temperature, err := strconv.ParseFloat(payload, 64)
        if err != nil {
            fmt.Println("Error parsing temperature:", err)
            return
        }
        if temperature < -50 || temperature > 50 {
            fmt.Println("Received invalid temperature value:", temperature)
            return
        }
        device.temperature_value = temperature
    }
 
    // Обновление значений в базе данных
    _, err = db.Exec("UPDATE devices SET gas_value=?, humidity_value=?, temperature_value=? WHERE name=?", device.gas_value, device.humidity_value, device.temperature_value, device.name)
    if err != nil {
        fmt.Println("Error updating database:", err)
        return
    }
    fmt.Println("Data updated for device:", device.name)
}
 
func updateDbValues(db *sql.DB) {
    opts := mqtt.NewClientOptions().AddBroker(broker)
    client := mqtt.NewClient(opts)
 
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }
 
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
 
    for range ticker.C {
        // Получение списка устройств из базы данных
        rows, err := db.Query("SELECT * FROM devices")
        if err != nil {
            panic(err)
        }
 
        // Перебор результатов запроса
        for rows.Next() {
            var device Device
            // Сканирование значений из результата запроса в структуру
            err := rows.Scan(&device.id, &device.name, &device.gas_topic, &device.humidity_topic, &device.temperature_topic, &device.gas_value, &device.humidity_value, &device.temperature_value)
            if err != nil {
                panic(err)
            }
 
            copiedDevice := device
 
            // Подписываемся на топики каждого типа данных для данного устройства
            topics := []string{device.gas_topic, device.humidity_topic, device.temperature_topic}
            for _, topic := range topics {
                token := client.Subscribe(topic, 0, func(client mqtt.Client, msg mqtt.Message) {
                    handleDataUpdate(client, msg, &copiedDevice, &mu, db, topic)
                })
                token.Wait()
                if token.Error() != nil {
                    fmt.Printf("Error subscribing to topic %s: %s\n", topic, token.Error())
                }
            }
        }
        if err := rows.Err(); err != nil {
            panic(err)
        }
    }
}
 
type device struct {
    ID               int
    GasValue         float64
    HumidityValue    float64
    TemperatureValue float64
}
 
func main() {
    var err error
    db, err := sql.Open("mysql", "wand:sdafksdfksfadksdfaksfdk!@!@#K!2@tcp(77.222.36.56:16499)/wand")
    if err != nil {
        log.Fatalf("Failed connect to db: %v", err)
    }
    defer db.Close()
    go updateDbValues(db)
 
    if err := db.Ping(); err != nil {
        log.Fatalf("Failed to connect to db: %v", err)
    }
 
    log.Println("Successfully connected to db")
 
    if err != nil {
        log.Fatalf("Failed to insert to DB")
    }
 
    r := gin.Default()
    host := "0.0.0.0"
    port := 8080
    addr := fmt.Sprintf("%s:%d", host, port)
    r.LoadHTMLGlob("html/*")
 
    r.GET("/", mainPageHandler)
    r.GET("/register", registerPageHandler)
    r.GET("/panel", isAuthenticated(), func(c *gin.Context) {
        username, _ := c.Get("username")
        fmt.Printf("user name: %s\n", username)
 
        role, err := getUserRole(db, username.(string))
        if err != nil {
            panic(err)
        }
 
        if role == "admin" {
            c.File("html/panel.html")
        } else {
            c.Redirect(http.StatusFound, "/account")
        }
    })
    r.GET("/account", isAuthenticated(), func(c *gin.Context) {
        username, _ := c.Get("username")
        fmt.Printf("user name: %s\n", username)
 
        tmpl, err := template.ParseFiles("html/main.html")
        if err != nil {
            fmt.Println("Error parsing template:", err)
            c.String(http.StatusInternalServerError, "Internal Server Error")
            return
        }
 
        deviceIDs, err := getDevicesUsers(db, username.(string))
        if err != nil {
            fmt.Println("Ошибка при получении списка устройств пользователя:", err)
            return
        }
 
        fmt.Println("Список устройств пользователя:")
        var devices []device
        for _, id := range deviceIDs {
            gasValue, humidityValue, temperatureValue, err := getValuesFromDevice(db, id)
            if err != nil {
                fmt.Printf("Ошибка при получении значений для устройства с ID %d: %v\n", id, err)
                continue
            }
            fmt.Printf("ID устройства: %d\n", id)
            fmt.Printf("Значение газа: %f\n", gasValue)
            fmt.Printf("Значение влажности: %f\n", humidityValue)
            fmt.Printf("Значение температуры: %f\n", temperatureValue)
            fmt.Println("---------------------------")
 
            // Добавляем устройство в слайс
            devices = append(devices, device{
                ID:               id,
                GasValue:         gasValue,
                HumidityValue:    humidityValue,
                TemperatureValue: temperatureValue,
            })
        }
 
        role, err := getUserRole(db, username.(string))
        if err != nil {
            panic(err)
        }
 
        isAdmin := (role == "admin")
 
        data := struct {
            Username string
            IsAdmin  bool
            Devices  []device
        }{
            Username: username.(string),
            IsAdmin:  isAdmin,
            Devices:  devices,
        }
 
        err = tmpl.Execute(c.Writer, data)
        if err != nil {
            fmt.Println("Error executing template:", err)
            c.String(http.StatusInternalServerError, "Internal Server Error")
            return
        }
    })
 
    r.GET("/api/devices", isAuthenticated(), func(c *gin.Context) {
        username, _ := c.Get("username")
 
        deviceIDs, err := getDevicesUsers(db, username.(string))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при получении списка устройств пользователя"})
            return
        }
 
        var devices []device
        for _, id := range deviceIDs {
            gasValue, humidityValue, temperatureValue, err := getValuesFromDevice(db, id)
            if err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Ошибка при получении значений для устройства с ID %d: %v", id, err)})
                return
            }
 
            // Добавляем устройство в слайс
            devices = append(devices, device{
                ID:               id,
                GasValue:         gasValue,
                HumidityValue:    humidityValue,
                TemperatureValue: temperatureValue,
            })
        }
 
        c.JSON(http.StatusOK, devices)
    })
 
    r.POST("/change_pass", isAuthenticated(), func(c *gin.Context) {
        changePasswordHandler(c, db)
    })
 
    r.POST("/register_handler", func(c *gin.Context) {
        registerDbHandler(c, db)
    })
 
    r.POST("/login_handler", func(c *gin.Context) {
        loginDbHandler(c, db)
    })
 
    r.POST("/exit_account", clearSessionData)
 
    r.NoRoute(noRoutePageHandler)
 
    fmt.Printf("Server listening on: %d", port)
 
    r.Run(addr)
}
 
func clearSessionData(c *gin.Context) {
    session, err := store.Get(c.Request, "session-name")
    if err != nil {
        // Обработка ошибки
        c.String(http.StatusInternalServerError, err.Error())
        return
    }
 
    delete(session.Values, "username")
    delete(session.Values, "authenticated")
 
    err = session.Save(c.Request, c.Writer)
    if err != nil {
        // Обработка ошибки
        c.String(http.StatusInternalServerError, err.Error())
        return
    }
 
    c.Redirect(http.StatusSeeOther, "/")
}
 
func mainPageHandler(c *gin.Context) {
    session, err := store.Get(c.Request, "session-name")
    if err != nil {
        c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Ошибка получения сессии"})
        return
    }
 
    authenticated, ok := session.Values["authenticated"].(bool)
    if ok && authenticated {
        c.Redirect(http.StatusFound, "/account")
        return
    }
 
    c.File("html/index.html")
}
 
func registerPageHandler(c *gin.Context) {
    c.File("html/register.html")
}
 
func noRoutePageHandler(c *gin.Context) {
    c.File("html/not_found.html")
}
 
func changePasswordHandler(c *gin.Context, db *sql.DB) {
    newPassword := c.PostForm("newPassword")
    username, _ := c.Get("username")
 
    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка хеширования пароля"})
        return
    }
 
    _, err = db.Exec("UPDATE users SET password = ? WHERE login = ?", hashedPassword, username)
    if err != nil {
        panic(err.Error())
    }
 
    c.JSON(http.StatusOK, gin.H{"message": "Пароль успешно изменён!"})
}
 
func registerDbHandler(c *gin.Context, db *sql.DB) {
    username := c.PostForm("usernameInput")
    password := c.PostForm("passwordInput")
    passwordRepeat := c.PostForm("passwordrepeatInput")
 
    if password != passwordRepeat {
        handleError(c, "Введеные пароли не совпадают")
        return
    }
 
    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        handleError(c, "Ошибка при хешировании пароля")
        return
    }
 
    var count int
    row := db.QueryRow("SELECT COUNT(*) FROM users WHERE login = ?", username)
    err = row.Scan(&count)
    if err != nil {
        handleError(c, "Ошибка при проверке имени пользователя")
        return
    }
    if count > 0 {
        handleError(c, "Пользователь с таким именем уже существует")
        return
    }
 
    var stmt *sql.Stmt
    stmt, err = db.Prepare("INSERT INTO users (login, password) VALUES (?, ?)")
    if err != nil {
        handleError(c, "Ошибка при подготовке пароля")
        return
    }
    defer stmt.Close()
 
    _, err = stmt.Exec(username, hashedPassword)
    if err != nil {
        handleError(c, "Не удалось зарегистрировать пользователя")
        return
    }
 
    log.Printf("Пользователь добавлен")
 
    c.Redirect(http.StatusFound, "/")
}
 
func handleError(c *gin.Context, errorMessage string) {
    c.HTML(http.StatusInternalServerError, "error.html", gin.H{"errorMessage": errorMessage})
}
 
func loginDbHandler(c *gin.Context, db *sql.DB) {
    username := c.PostForm("loginInput")
    password := c.PostForm("passwordInput")
 
    var hashedPassword string
    err := db.QueryRow("SELECT password FROM users WHERE login = ?", username).Scan(&hashedPassword)
    if err != nil {
        handleError(c, "Неправильное имя пользователя или пароль")
        return
    }
 
    err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
    if err != nil {
        c.Redirect(http.StatusOK, "/")
        handleError(c, "Неправильное имя пользователя или пароль")
        return
    }
 
    session, err := store.New(c.Request, "session-name")
    if err != nil {
        handleError(c, "Ошибка создания сессии")
        return
    }
    defer session.Save(c.Request, c.Writer)
 
    session.Values["username"] = username
    session.Values["authenticated"] = true
 
    if err := session.Save(c.Request, c.Writer); err != nil {
        handleError(c, "Ошибка сохранения сессии")
        return
    }
 
    c.Redirect(http.StatusFound, "/account")
}
 
func isAuthenticated() gin.HandlerFunc {
    return func(c *gin.Context) {
        session, err := store.Get(c.Request, "session-name")
        if err != nil {
            handleError(c, "Ошибка получения сессии")
            return
        }
 
        authenticated, ok := session.Values["authenticated"].(bool)
        if !ok || !authenticated {
            handleError(c, "Необходима аунтефикация")
            return
        }
 
        c.Set("username", session.Values["username"])
        c.Next()
    }
}
 
func getUserRole(db *sql.DB, name string) (string, error) {
    query := "SELECT Role FROM users WHERE login = ?"
 
    var role string
    err := db.QueryRow(query, name).Scan(&role)
 
    if err != nil {
        return "", err
    }
 
    return role, nil
}
 
func getValuesFromDevice(db *sql.DB, id int) (float64, float64, float64, error) {
    row := db.QueryRow("SELECT gas_value, humidity_value, temperature_value FROM devices WHERE id = ?", id)
 
    var gasValue, humidityValue, temperatureValue float64
 
    err := row.Scan(&gasValue, &humidityValue, &temperatureValue)
    if err != nil {
        return 0, 0, 0, err
    }
 
    return gasValue, humidityValue, temperatureValue, nil
}
 
func getDevicesUsers(db *sql.DB, name string) ([]int, error) {
    rows, err := db.Query("SELECT device_id FROM user_devices JOIN users ON user_devices.user_id = users.id WHERE users.login = ?", name)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
 
    var deviceIDs []int
 
    for rows.Next() {
        var deviceID int
        if err := rows.Scan(&deviceID); err != nil {
            return nil, err
        }
        deviceIDs = append(deviceIDs, deviceID)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
 
    return deviceIDs, nil
}
