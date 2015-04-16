var LoginController = (function() {
	var user = localStorage.getItem("user");
	var key = localStorage.getItem("key");

	var loggedIn = function() {
		return (user && key);
	};

	var loginForm = $("#login-form");
	var logOutDiv = $("#log-out-div");
	var loginButton = $("#login-button");
	var logOutButton = $("#log-out-button");
	var loginIncorrect = $("#login-incorrect");
	var loginEmail = $("#login-email");
	var loginPass = $("#login-pass");
	var accountPannelLink = $("#account-pannel-link");

	var loginQuery = function(loginUser, loginKey) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/get_groups_by_user',
			data: {
				u: loginUser,
				uk: loginKey,
			},
			success: function(data) {
				user = loginEmail.val();
				key = loginPass.val();
				loginEmail.val('');
				loginPass.val('');
				loginIncorrect.hide();
				doLogin();

				accountPannelLink.show();
			},
			error: function() {
				loginIncorrect.show();
			},
			dataType: 'json',
		});
	};

	var doLogin = function () {
		localStorage.setItem("user", user);
		localStorage.setItem("key", key);
		logOutDiv.show();
		loginForm.hide();
	};

	var logOut = function () {
		localStorage.removeItem("user");
		localStorage.removeItem("key");
		logOutDiv.hide();
		loginForm.show();
		window.location = 'index.html';
	};

	logOutButton.click(logOut);

	if (loggedIn()) {
		doLogin();
		loginQuery(user, key);
	} else {
		loginForm.show();
		loginForm.submit(function(event) {
			loginQuery(loginEmail.val(), loginPass.val());
			event.preventDefault();
		});
	}
})();
