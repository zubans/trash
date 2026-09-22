MANIFEST = {
    "title": "Комиссия умножается на VALUE",
    "description": "Ставка по уровню умножается на VALUE: 0.5 — вдвое меньше. " +
                   "Обещание «вдвое» держится при любой базовой ставке.",
    "defaults": {"VALUE": 0.5},
}

def rate(f):
    return f.level_percent * f.config["VALUE"]
