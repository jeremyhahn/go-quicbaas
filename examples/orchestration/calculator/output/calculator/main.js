// Get the display element
const display = document.getElementById('display');
// Get the button elements
const buttons = document.querySelectorAll('button');
// Define the Calculator class
let calculator = {
    add(a, b) {
        return a + b;
    },
    subtract(a, b) {
        return a - b;
    },
    multiply(a, b) {
        return a * b;
    },
    divide(a, b) {
        if (b === 0) {
            throw new Error("Cannot divide by zero");
        }
        return a / b;
    }
};
// Add event listeners to the buttons
buttons.forEach((button) => {
    button.addEventListener('click', () => {
        const value = button.textContent;
        if (value === '=') {
            // Evaluate the expression and display the result
            try {
                let num1 = display.value.match("\d+")
                console.log(num1)
                display.value = eval(display.value);
            } catch{
               console.log('error')
            }
        } else if (value === 'C') {
            // Clear the display
            display.value = '';
        } else {
            // Append the value to the display
            display.value += value;
        }
    });
});