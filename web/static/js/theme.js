const themePicker = document.getElementById('nps-theme');
const themeStorageKey = 'nps-admin-theme';

function applyTheme(value) {
    if (!Array.from(themePicker.options).some(function (option) { return option.value === value; })) {
        value = 'classic';
    }
    if (value === 'classic') {
        delete document.documentElement.dataset.npsTheme;
    } else {
        document.documentElement.dataset.npsTheme = value;
    }
    themePicker.value = value;
}

try {
    applyTheme(localStorage.getItem(themeStorageKey));
} catch (error) {
    applyTheme('classic');
}

themePicker.addEventListener('change', function () {
    applyTheme(themePicker.value);
    try {
        localStorage.setItem(themeStorageKey, themePicker.value);
    } catch (error) {
        // The selection still applies on this page when browser storage is disabled.
    }
});
