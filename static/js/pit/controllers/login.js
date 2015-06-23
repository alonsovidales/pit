var LoginController = (function() {
	var user = localStorage.getItem("user");
	var key = localStorage.getItem("key");

	var loggedIn = function() {
		return (user && key && user !== "undefined" && key !== "undefined");
	};

	var loginForm = $("#login-dropdown");
	var logOutDiv = $("#logout-container");
	var loginButton = $("#login-button");
	var logOutButton = $("#log-out-button");
	var loginIncorrect = $("#login-incorrect");
	var loginEmail = $("#login-email");
	var loginPass = $("#login-pass");
	var accountName = $("#account-name-top");
	var accountPannelLink = $("#account-pannel-link");

	var loginQuery = function(loginUser, loginKey) {
		user = loginUser;
		key = loginKey;
		GroupsController.getGroupsInfo(function(data) {
			loginEmail.val('');
			loginPass.val('');
			loginIncorrect.hide();
			doLogin();

			accountPannelLink.show();
		}, function () {
			loginIncorrect.show();
		});
	};

	var doLogin = function () {
		localStorage.setItem("user", user);
		localStorage.setItem("key", key);
		accountName.text(user);
		accountName.show();
		logOutDiv.show();
		loginForm.hide();
	};

	var logOut = function () {
		localStorage.removeItem("user");
		localStorage.removeItem("key");
		accountName.hide();
		logOutDiv.hide();
		loginForm.show();
		window.location = 'index.html';
	};

	logOutButton.click(logOut);

	if (loggedIn()) {
		$(function() {
			$('#try-it-button').hide();
			doLogin();
			loginQuery(user, key);
		});
	} else {
		loginForm.show();
		loginForm.submit(function(event) {
			loginQuery(loginEmail.val(), loginPass.val());
			event.preventDefault();
		});
	}

	return {
		getUser: function() {
			return user;
		},
		getKey: function() {
			return key;
		}
	};
})();
