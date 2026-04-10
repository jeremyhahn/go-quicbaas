(function () {
    "use strict";

    var board = document.getElementById("board");
    var status = document.getElementById("status");
    var newGameBtn = document.getElementById("new-game");
    var difficultySelect = document.getElementById("difficulty");
    var cells = board.querySelectorAll(".cell");
    var locked = false;

    function renderBoard(state) {
        cells.forEach(function (cell) {
            var r = parseInt(cell.dataset.row, 10);
            var c = parseInt(cell.dataset.col, 10);
            var val = state.board[r][c];

            cell.textContent = val === "." ? "" : val;
            cell.classList.remove("x", "o", "taken");

            if (val === "X") {
                cell.classList.add("x", "taken");
            } else if (val === "O") {
                cell.classList.add("o", "taken");
            }
        });
    }

    function updateStatus(state) {
        status.classList.remove("win", "lose", "draw");

        if (state.state === "X wins") {
            status.textContent = "You win!";
            status.classList.add("win");
            lockBoard();
        } else if (state.state === "O wins") {
            status.textContent = "AI wins!";
            status.classList.add("lose");
            lockBoard();
        } else if (state.state === "draw") {
            status.textContent = "Draw!";
            status.classList.add("draw");
            lockBoard();
        } else {
            status.textContent = "Your turn (X)";
            unlockBoard();
        }
    }

    function lockBoard() {
        locked = true;
        cells.forEach(function (cell) {
            cell.classList.add("locked");
        });
    }

    function unlockBoard() {
        locked = false;
        cells.forEach(function (cell) {
            cell.classList.remove("locked");
        });
    }

    function applyState(state) {
        renderBoard(state);
        updateStatus(state);
        difficultySelect.value = state.difficulty;
    }

    function apiRequest(url, body) {
        return fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
        }).then(function (res) {
            return res.json().then(function (data) {
                if (!res.ok) {
                    throw new Error(data.error || "Request failed");
                }
                return data;
            });
        });
    }

    function handleCellClick(e) {
        if (locked) return;

        var cell = e.currentTarget;
        if (cell.classList.contains("taken")) return;

        var row = parseInt(cell.dataset.row, 10);
        var col = parseInt(cell.dataset.col, 10);

        locked = true;
        apiRequest("/api/play", { row: row, col: col })
            .then(applyState)
            .catch(function (err) {
                status.textContent = "Error: " + err.message;
                locked = false;
            });
    }

    function handleNewGame() {
        var difficulty = difficultySelect.value;
        apiRequest("/api/reset", { difficulty: difficulty })
            .then(applyState)
            .catch(function (err) {
                status.textContent = "Error: " + err.message;
            });
    }

    // Wire up event listeners.
    cells.forEach(function (cell) {
        cell.addEventListener("click", handleCellClick);
    });
    newGameBtn.addEventListener("click", handleNewGame);

    // Load initial state.
    fetch("/api/state")
        .then(function (res) { return res.json(); })
        .then(applyState)
        .catch(function (err) {
            status.textContent = "Failed to load game: " + err.message;
        });
})();
